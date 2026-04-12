package openai

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

type Config struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	HTTPClient   *http.Client
	Timeout      time.Duration
	MaxRetries   int
	RetryBackoff time.Duration
}

type Provider struct {
	baseURL      string
	apiKey       string
	defaultModel string
	httpClient   *http.Client
	timeout      time.Duration
	maxRetries   int
	retryBackoff time.Duration
}

type requestPayload struct {
	Model    string                 `json:"model"`
	Messages []provider.ChatMessage `json:"messages"`
	Stream   bool                   `json:"stream,omitempty"`
}

type responsePayload struct {
	ID      string                `json:"id"`
	Object  string                `json:"object"`
	Created int64                 `json:"created"`
	Model   string                `json:"model"`
	Choices []responseChoice      `json:"choices"`
	Usage   provider.Usage        `json:"usage"`
	Error   *responseErrorPayload `json:"error,omitempty"`
}

type responseChoice struct {
	Index        int                  `json:"index"`
	Message      provider.ChatMessage `json:"message"`
	Delta        provider.ChatMessage `json:"delta"`
	FinishReason string               `json:"finish_reason"`
}

type responseErrorPayload struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

type modelsResponsePayload struct {
	Object string           `json:"object"`
	Data   []provider.Model `json:"data"`
}

type streamState struct {
	events        chan<- provider.ChatCompletionStreamEvent
	attemptHandle provider.AttemptHandle
	streamModel   string
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

	return Provider{
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:       strings.TrimSpace(cfg.APIKey),
		defaultModel: strings.TrimSpace(cfg.DefaultModel),
		httpClient:   client,
		timeout:      timeout,
		maxRetries:   maxRetries,
		retryBackoff: retryBackoff,
	}
}

func (p Provider) CreateChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (provider.ChatCompletionResponse, error) {
	payload, err := p.doJSONRequest(ctx, requestPayload{
		Model:    p.resolveModel(req.Model),
		Messages: req.Messages,
		Stream:   false,
	}, false)
	if err != nil {
		return provider.ChatCompletionResponse{}, err
	}

	return provider.ChatCompletionResponse{
		ID:      payload.ID,
		Object:  payload.Object,
		Created: payload.Created,
		Model:   payload.Model,
		Choices: toProviderChoices(payload.Choices),
		Usage:   payload.Usage,
	}, nil
}

func (p Provider) StreamChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (<-chan provider.ChatCompletionStreamEvent, error) {
	resp, attemptHandle, err := p.openStream(ctx, requestPayload{
		Model:    p.resolveModel(req.Model),
		Messages: req.Messages,
		Stream:   true,
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
			events:        events,
			attemptHandle: attemptHandle,
			streamModel:   p.resolveModel(req.Model),
		}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					state.publishFailure(ctx, errors.New("upstream stream ended before [DONE]"))
					return
				}
				state.publishFailure(ctx, err)
				return
			}

			done, ok := state.consumeLine(ctx, line)
			if done || !ok {
				return
			}
		}
	}()

	return events, nil
}

func (s *streamState) consumeLine(ctx context.Context, line string) (bool, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, ":") || !strings.HasPrefix(trimmed, "data: ") {
		return false, true
	}

	data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data: "))
	if data == "[DONE]" {
		return true, false
	}

	payload, err := decodeStreamPayload(data)
	if err != nil {
		s.publishFailure(ctx, err)
		return false, false
	}
	if strings.TrimSpace(payload.Model) != "" {
		s.streamModel = payload.Model
	}

	if !s.publishChunk(ctx, payload) {
		return false, false
	}
	return false, true
}

func decodeStreamPayload(data string) (responsePayload, error) {
	var payload responsePayload
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return responsePayload{}, err
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return responsePayload{}, errors.New(payload.Error.Message)
	}
	return payload, nil
}

func (s *streamState) publishChunk(ctx context.Context, payload responsePayload) bool {
	select {
	case <-ctx.Done():
		s.publishFailure(ctx, ctx.Err())
		return false
	case s.events <- provider.ChatCompletionStreamEvent{
		Chunk: provider.ChatCompletionChunk{
			ID:      payload.ID,
			Object:  payload.Object,
			Created: payload.Created,
			Model:   payload.Model,
			Choices: toProviderStreamChoices(payload.Choices),
		},
	}:
		return true
	}
}

func (s *streamState) publishFailure(ctx context.Context, err error) {
	_ = failAttempt(ctx, s.attemptHandle, s.streamModel)
	publishStreamError(ctx, s.events, err)
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
	return payload.Data, nil
}

func (p Provider) doJSONRequest(ctx context.Context, payload requestPayload, stream bool) (responsePayload, error) {
	ctx, cancel := p.withTimeout(ctx)
	defer cancel()

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return responsePayload{}, err
	}

	accept := ""
	if stream {
		accept = "text/event-stream"
	}

	attemptMetadata := &provider.AttemptMetadata{
		Backend:      provider.AttemptBackendFromContext(ctx),
		Model:        payload.Model,
		Stream:       stream,
		PromptTokens: provider.EstimatePromptTokens(payload.Messages),
		CreatedAt:    time.Now(),
	}

	resp, attemptHandle, err := p.doRequest(ctx, attemptMetadata, http.MethodPost, p.endpointURL(), reqBody, accept)
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
	if attemptHandle != nil {
		if err := attemptHandle.Complete(ctx, provider.AttemptResult{
			Model:            result.Model,
			Status:           "succeeded",
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
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
		PromptTokens: provider.EstimatePromptTokens(payload.Messages),
		CreatedAt:    time.Now(),
	}

	resp, attemptHandle, err := p.doRequest(ctx, attemptMetadata, http.MethodPost, p.endpointURL(), reqBody, "text/event-stream")
	if err != nil {
		return nil, nil, err
	}
	return resp, attemptHandle, nil
}

func (p Provider) endpointURL() string {
	return p.baseURL + "/chat/completions"
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

func toProviderChoices(choices []responseChoice) []provider.ChatChoice {
	result := make([]provider.ChatChoice, 0, len(choices))
	for _, choice := range choices {
		result = append(result, provider.ChatChoice{
			Index:        choice.Index,
			Message:      choice.Message,
			FinishReason: choice.FinishReason,
		})
	}
	return result
}

func toProviderStreamChoices(choices []responseChoice) []provider.ChatChoice {
	result := make([]provider.ChatChoice, 0, len(choices))
	for _, choice := range choices {
		message := choice.Message
		if message.Role == "" && message.Content == "" {
			message = choice.Delta
		}

		result = append(result, provider.ChatChoice{
			Index:        choice.Index,
			Message:      message,
			FinishReason: choice.FinishReason,
		})
	}
	return result
}

func readHTTPError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	var payload responsePayload
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error != nil && payload.Error.Message != "" {
		return fmt.Errorf("upstream status %d: %s", resp.StatusCode, payload.Error.Message)
	}

	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	return fmt.Errorf("upstream status %d: %s", resp.StatusCode, text)
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
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
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
