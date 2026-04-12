package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
)

const (
	defaultAPIVersion          = "2023-06-01"
	defaultMaxTokens           = 1024
	sseEventPrefix             = "event:"
	upstreamStatusDetailFormat = "upstream status %d: %s"
)

type Config struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	APIVersion   string
	MaxTokens    int
	HTTPClient   *http.Client
	Timeout      time.Duration
	MaxRetries   int
	RetryBackoff time.Duration
}

type Provider struct {
	baseURL      string
	apiKey       string
	defaultModel string
	apiVersion   string
	maxTokens    int
	httpClient   *http.Client
	timeout      time.Duration
	maxRetries   int
	retryBackoff time.Duration
}

type requestPayload struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system,omitempty"`
	Messages  []requestMessage `json:"messages"`
	Stream    bool             `json:"stream,omitempty"`
}

type requestMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responsePayload struct {
	ID         string                `json:"id"`
	Type       string                `json:"type"`
	Role       string                `json:"role"`
	Content    []contentBlock        `json:"content"`
	Model      string                `json:"model"`
	StopReason string                `json:"stop_reason"`
	Usage      usagePayload          `json:"usage"`
	Error      *responseErrorPayload `json:"error,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type usagePayload struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type responseErrorPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type modelsResponsePayload struct {
	Data []modelPayload `json:"data"`
}

type modelPayload struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
}

type streamEventPayload struct {
	Type    string                `json:"type"`
	Message *responsePayload      `json:"message,omitempty"`
	Delta   streamDeltaPayload    `json:"delta,omitempty"`
	Error   *responseErrorPayload `json:"error,omitempty"`
}

type streamDeltaPayload struct {
	Type       string `json:"type,omitempty"`
	Text       string `json:"text,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
}

type streamState struct {
	ctx                 context.Context
	events              chan<- provider.ChatCompletionStreamEvent
	attemptHandle       provider.AttemptHandle
	streamCreated       int64
	streamID            string
	streamModel         string
	pendingRole         string
	roleEmitted         bool
	finishReasonEmitted bool
}

type sseEvent struct {
	name string
	data string
}

func New(cfg Config) Provider {
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	timeout := cfg.Timeout
	if timeout < 0 {
		timeout = 0
	}

	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	retryBackoff := cfg.RetryBackoff
	if retryBackoff <= 0 {
		retryBackoff = 200 * time.Millisecond
	}

	apiVersion := strings.TrimSpace(cfg.APIVersion)
	if apiVersion == "" {
		apiVersion = defaultAPIVersion
	}

	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	return Provider{
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:       strings.TrimSpace(cfg.APIKey),
		defaultModel: strings.TrimSpace(cfg.DefaultModel),
		apiVersion:   apiVersion,
		maxTokens:    maxTokens,
		httpClient:   client,
		timeout:      timeout,
		maxRetries:   maxRetries,
		retryBackoff: retryBackoff,
	}
}

func (p Provider) CreateChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (provider.ChatCompletionResponse, error) {
	systemPrompt, messages := splitMessages(req.Messages)
	payload, err := p.doJSONRequest(ctx, requestPayload{
		Model:     p.resolveModel(req.Model),
		MaxTokens: p.maxTokens,
		System:    systemPrompt,
		Messages:  toRequestMessages(messages),
	})
	if err != nil {
		return provider.ChatCompletionResponse{}, err
	}

	content := extractTextContent(payload.Content)
	createdAt := time.Now().UTC().Unix()
	return provider.ChatCompletionResponse{
		ID:      payload.ID,
		Object:  "chat.completion",
		Created: createdAt,
		Model:   payload.Model,
		Choices: []provider.ChatChoice{{
			Index: 0,
			Message: provider.ChatMessage{
				Role:    payload.Role,
				Content: content,
			},
			FinishReason: finishReason(payload.StopReason),
		}},
		Usage: provider.Usage{
			PromptTokens:     payload.Usage.InputTokens,
			CompletionTokens: payload.Usage.OutputTokens,
			TotalTokens:      payload.Usage.InputTokens + payload.Usage.OutputTokens,
		},
	}, nil
}

func (p Provider) StreamChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (<-chan provider.ChatCompletionStreamEvent, error) {
	systemPrompt, messages := splitMessages(req.Messages)
	resp, attemptHandle, err := p.openStream(ctx, requestPayload{
		Model:     p.resolveModel(req.Model),
		MaxTokens: p.maxTokens,
		System:    systemPrompt,
		Messages:  toRequestMessages(messages),
		Stream:    true,
	})
	if err != nil {
		return nil, err
	}

	events := make(chan provider.ChatCompletionStreamEvent)
	go func() {
		defer close(events)
		defer func() {
			_ = resp.Body.Close()
		}()

		reader := bufio.NewReader(resp.Body)
		state := streamState{
			ctx:           ctx,
			events:        events,
			attemptHandle: attemptHandle,
			streamCreated: time.Now().UTC().Unix(),
			streamID:      fmt.Sprintf("anthropic-%d", time.Now().UTC().UnixNano()),
			streamModel:   p.resolveModel(req.Model),
		}

		for {
			event, err := readSSEEvent(reader)
			if err != nil {
				if errors.Is(err, io.EOF) {
					state.publishFailure(errors.New("upstream stream ended before message_stop"))
					return
				}
				state.publishFailure(err)
				return
			}
			if strings.TrimSpace(event.data) == "" {
				continue
			}
			done, ok := state.consumeEvent(event)
			if done || !ok {
				return
			}
		}
	}()

	return events, nil
}

func (s *streamState) consumeEvent(event sseEvent) (bool, bool) {
	payload, err := decodeStreamEventPayload(event.data)
	if err != nil {
		s.publishFailure(err)
		return false, false
	}

	switch streamEventKind(event.name, payload.Type) {
	case "ping", "content_block_start", "content_block_stop":
		return false, true
	case "message_start":
		s.consumeMessageStart(payload)
		return false, true
	case "content_block_delta":
		return false, s.publishContentDelta(payload)
	case "message_delta":
		return false, s.publishMessageDelta(payload)
	case "message_stop":
		return true, s.publishMessageStop()
	case "error":
		s.publishFailure(errors.New("anthropic stream error"))
		return false, false
	default:
		return false, true
	}
}

func decodeStreamEventPayload(data string) (streamEventPayload, error) {
	var payload streamEventPayload
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return streamEventPayload{}, err
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return streamEventPayload{}, errors.New(payload.Error.Message)
	}
	return payload, nil
}

func streamEventKind(eventName string, payloadType string) string {
	kind := strings.TrimSpace(eventName)
	if kind != "" {
		return kind
	}
	return strings.TrimSpace(payloadType)
}

func (s *streamState) consumeMessageStart(payload streamEventPayload) {
	if payload.Message == nil {
		return
	}
	if strings.TrimSpace(payload.Message.ID) != "" {
		s.streamID = payload.Message.ID
	}
	if strings.TrimSpace(payload.Message.Model) != "" {
		s.streamModel = payload.Message.Model
	}
	if strings.TrimSpace(payload.Message.Role) != "" {
		s.pendingRole = payload.Message.Role
		s.roleEmitted = false
	}
}

func (s *streamState) publishContentDelta(payload streamEventPayload) bool {
	if strings.TrimSpace(payload.Delta.Type) != "text_delta" || payload.Delta.Text == "" {
		return true
	}

	message := provider.ChatMessage{Content: payload.Delta.Text}
	if !s.roleEmitted && s.pendingRole != "" {
		message.Role = s.pendingRole
		s.roleEmitted = true
	}
	return s.publishChunk(message, "")
}

func (s *streamState) publishMessageDelta(payload streamEventPayload) bool {
	if payload.Delta.StopReason == "" {
		return true
	}
	s.finishReasonEmitted = true
	return s.publishChunk(provider.ChatMessage{}, finishReason(payload.Delta.StopReason))
}

func (s *streamState) publishMessageStop() bool {
	if s.finishReasonEmitted {
		return true
	}
	return s.publishChunk(provider.ChatMessage{}, "stop")
}

func (s *streamState) publishChunk(message provider.ChatMessage, finishReason string) bool {
	return publishStreamChunk(s.ctx, s.events, s.streamID, s.streamModel, s.streamCreated, message, finishReason)
}

func (s *streamState) publishFailure(err error) {
	_ = failAttempt(s.ctx, s.attemptHandle, s.streamModel)
	publishStreamError(s.ctx, s.events, err)
}

func (p Provider) ListModels(ctx context.Context) ([]provider.Model, error) {
	ctx, cancel := p.withTimeout(ctx)
	defer cancel()

	resp, _, err := p.doRequest(ctx, nil, http.MethodGet, p.modelsEndpointURL(), nil, "")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var payload modelsResponsePayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	models := make([]provider.Model, 0, len(payload.Data))
	for _, model := range payload.Data {
		if strings.TrimSpace(model.ID) == "" {
			continue
		}
		models = append(models, provider.Model{
			ID:      model.ID,
			Object:  "model",
			Created: parseTimestamp(model.CreatedAt).Unix(),
			OwnedBy: "anthropic",
		})
	}
	return models, nil
}

func toRequestMessages(messages []provider.ChatMessage) []requestMessage {
	result := make([]requestMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, requestMessage{
			Role:    message.Role,
			Content: message.Content,
		})
	}
	return result
}

func splitMessages(messages []provider.ChatMessage) (string, []provider.ChatMessage) {
	var systemParts []string
	translated := make([]provider.ChatMessage, 0, len(messages))
	for _, message := range messages {
		if strings.EqualFold(strings.TrimSpace(message.Role), "system") {
			content := strings.TrimSpace(message.Content)
			if content != "" {
				systemParts = append(systemParts, content)
			}
			continue
		}
		translated = append(translated, message)
	}

	return strings.Join(systemParts, "\n\n"), translated
}

func extractTextContent(blocks []contentBlock) string {
	var builder strings.Builder
	for _, block := range blocks {
		if strings.TrimSpace(block.Type) != "text" {
			continue
		}
		builder.WriteString(block.Text)
	}
	return builder.String()
}

func finishReason(stopReason string) string {
	trimmed := strings.TrimSpace(stopReason)
	if trimmed == "" {
		return "stop"
	}
	switch trimmed {
	case "end_turn":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		return trimmed
	}
}

func parseTimestamp(value string) time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Unix(0, 0).UTC()
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return time.Unix(0, 0).UTC()
	}
	return parsed.UTC()
}

func publishStreamChunk(ctx context.Context, events chan<- provider.ChatCompletionStreamEvent, id string, model string, created int64, message provider.ChatMessage, finishReason string) bool {
	select {
	case <-ctx.Done():
		publishStreamError(ctx, events, ctx.Err())
		return false
	case events <- provider.ChatCompletionStreamEvent{
		Chunk: provider.ChatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
			Choices: []provider.ChatChoice{{
				Index:        0,
				Message:      message,
				FinishReason: finishReason,
			}},
		},
	}:
		return true
	}
}

func readSSEEvent(reader *bufio.Reader) (sseEvent, error) {
	var eventName string
	var dataLines []string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				eventName, dataLines = consumeSSELine(strings.TrimRight(line, "\r\n"), eventName, dataLines)
				if eventName != "" || len(dataLines) > 0 {
					return sseEvent{name: eventName, data: strings.Join(dataLines, "\n")}, nil
				}
			}
			return sseEvent{}, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if eventName == "" && len(dataLines) == 0 {
				continue
			}
			return sseEvent{name: eventName, data: strings.Join(dataLines, "\n")}, nil
		}
		eventName, dataLines = consumeSSELine(line, eventName, dataLines)
	}
}

func consumeSSELine(line string, eventName string, dataLines []string) (string, []string) {
	switch {
	case strings.HasPrefix(line, ":"):
		return eventName, dataLines
	case strings.HasPrefix(line, sseEventPrefix):
		return strings.TrimSpace(strings.TrimPrefix(line, sseEventPrefix)), dataLines
	case strings.HasPrefix(line, "data:"):
		return eventName, append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
	default:
		return eventName, dataLines
	}
}

func (p Provider) doJSONRequest(ctx context.Context, payload requestPayload) (responsePayload, error) {
	ctx, cancel := p.withTimeout(ctx)
	defer cancel()

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return responsePayload{}, err
	}

	attemptMetadata := &provider.AttemptMetadata{
		Backend:      provider.AttemptBackendFromContext(ctx),
		Model:        payload.Model,
		Stream:       false,
		PromptTokens: estimateAttemptPromptTokens(payload),
		CreatedAt:    time.Now(),
	}

	resp, attemptHandle, err := p.doRequest(ctx, attemptMetadata, http.MethodPost, p.messagesEndpointURL(), reqBody, "")
	if err != nil {
		return responsePayload{}, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var result responsePayload
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		if completeErr := failAttempt(ctx, attemptHandle, payload.Model); completeErr != nil {
			return responsePayload{}, completeErr
		}
		return responsePayload{}, err
	}
	if result.Error != nil && result.Error.Message != "" {
		if completeErr := failAttempt(ctx, attemptHandle, payload.Model); completeErr != nil {
			return responsePayload{}, completeErr
		}
		return responsePayload{}, errors.New(result.Error.Message)
	}
	if attemptHandle != nil {
		if err := attemptHandle.Complete(ctx, provider.AttemptResult{
			Model:            result.Model,
			Status:           "succeeded",
			PromptTokens:     result.Usage.InputTokens,
			CompletionTokens: result.Usage.OutputTokens,
			TotalTokens:      result.Usage.InputTokens + result.Usage.OutputTokens,
		}); err != nil {
			return responsePayload{}, provider.WrapAttemptAccountingError(err)
		}
	}
	return result, nil
}

func (p Provider) openStream(ctx context.Context, payload requestPayload) (*http.Response, provider.AttemptHandle, error) {
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}

	attemptMetadata := &provider.AttemptMetadata{
		Backend:      provider.AttemptBackendFromContext(ctx),
		Model:        payload.Model,
		Stream:       true,
		PromptTokens: estimateAttemptPromptTokens(payload),
		CreatedAt:    time.Now(),
	}

	resp, attemptHandle, err := p.doRequest(ctx, attemptMetadata, http.MethodPost, p.messagesEndpointURL(), reqBody, "text/event-stream")
	if err != nil {
		return nil, nil, err
	}
	return resp, attemptHandle, nil
}

func (p Provider) messagesEndpointURL() string {
	return p.baseURL + "/messages"
}

func (p Provider) modelsEndpointURL() string {
	return p.baseURL + "/models"
}

func (p Provider) resolveModel(model string) string {
	model = strings.TrimSpace(model)
	if model != "" {
		return model
	}
	return p.defaultModel
}

func readHTTPError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	var payload responsePayload
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != nil && payload.Error.Message != "" {
		return fmt.Errorf(upstreamStatusDetailFormat, resp.StatusCode, payload.Error.Message)
	}

	var fallback struct {
		Error *responseErrorPayload `json:"error"`
	}
	if err := json.Unmarshal(body, &fallback); err == nil && fallback.Error != nil && fallback.Error.Message != "" {
		return fmt.Errorf(upstreamStatusDetailFormat, resp.StatusCode, fallback.Error.Message)
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	return fmt.Errorf(upstreamStatusDetailFormat, resp.StatusCode, text)
}

func (p Provider) doRequest(ctx context.Context, metadata *provider.AttemptMetadata, method string, url string, body []byte, accept string) (*http.Response, provider.AttemptHandle, error) {
	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		attemptHandle, err := beginAttempt(ctx, metadata)
		if err != nil {
			return nil, nil, err
		}

		resp, err := p.doRequestOnce(ctx, method, url, body, accept)
		if err == nil {
			return resp, attemptHandle, nil
		}
		if completeErr := failAttempt(ctx, attemptHandle, metadataModel(metadata)); completeErr != nil {
			return nil, nil, completeErr
		}

		lastErr = err
		if attempt == p.maxRetries || !shouldRetryRequest(ctx, err) {
			return nil, nil, err
		}
		if err := p.waitRetry(ctx, attempt); err != nil {
			return nil, nil, lastErr
		}
	}
	return nil, nil, lastErr
}

func (p Provider) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if p.timeout <= 0 {
		return ctx, func() {
			// No timeout was configured, so there is no derived context to cancel.
		}
	}
	return context.WithTimeout(ctx, p.timeout)
}

func (p Provider) doRequestOnce(ctx context.Context, method string, url string, body []byte, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if p.apiKey != "" {
		req.Header.Set("x-api-key", p.apiKey)
	}
	if p.apiVersion != "" {
		req.Header.Set("anthropic-version", p.apiVersion)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 400 {
		return resp, nil
	}

	defer func() {
		_ = resp.Body.Close()
	}()
	upstreamErr := readHTTPError(resp)
	if shouldRetryHTTPStatus(resp.StatusCode) {
		return nil, retryableError{cause: upstreamErr}
	}
	return nil, upstreamErr
}

func (p Provider) waitRetry(ctx context.Context, attempt int) error {
	delay := p.retryBackoff * time.Duration(attempt+1)
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type retryableError struct {
	cause error
}

func (e retryableError) Error() string {
	return e.cause.Error()
}

func (e retryableError) Unwrap() error {
	return e.cause
}

func shouldRetryRequest(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return false
	}
	var retryable retryableError
	if errors.As(err, &retryable) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

func shouldRetryHTTPStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func publishStreamError(ctx context.Context, events chan<- provider.ChatCompletionStreamEvent, err error) {
	if err == nil {
		return
	}

	select {
	case <-ctx.Done():
	case events <- provider.ChatCompletionStreamEvent{Err: err}:
	}
}

func estimateAttemptPromptTokens(payload requestPayload) int {
	messages := make([]provider.ChatMessage, 0, len(payload.Messages)+1)
	if strings.TrimSpace(payload.System) != "" {
		messages = append(messages, provider.ChatMessage{
			Role:    "system",
			Content: payload.System,
		})
	}
	for _, message := range payload.Messages {
		messages = append(messages, provider.ChatMessage{
			Role:    message.Role,
			Content: message.Content,
		})
	}
	return provider.EstimatePromptTokens(messages)
}

func beginAttempt(ctx context.Context, metadata *provider.AttemptMetadata) (provider.AttemptHandle, error) {
	if metadata == nil {
		return nil, nil
	}
	recorder := provider.AttemptRecorderFromContext(ctx)
	if recorder == nil {
		return nil, nil
	}
	return recorder.BeginAttempt(ctx, *metadata)
}

func failAttempt(ctx context.Context, handle provider.AttemptHandle, model string) error {
	if handle == nil {
		return nil
	}
	if err := handle.Complete(ctx, provider.AttemptResult{
		Model:  model,
		Status: "failed",
	}); err != nil {
		return provider.WrapAttemptAccountingError(err)
	}
	return nil
}

func metadataModel(metadata *provider.AttemptMetadata) string {
	if metadata == nil {
		return ""
	}
	return metadata.Model
}
