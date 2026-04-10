package ollama

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
	Stream   bool                   `json:"stream"`
}

type responsePayload struct {
	Model           string               `json:"model"`
	CreatedAt       string               `json:"created_at"`
	Message         provider.ChatMessage `json:"message"`
	Done            bool                 `json:"done"`
	DoneReason      string               `json:"done_reason,omitempty"`
	PromptEvalCount int                  `json:"prompt_eval_count,omitempty"`
	EvalCount       int                  `json:"eval_count,omitempty"`
	Error           string               `json:"error,omitempty"`
}

type modelsResponsePayload struct {
	Models []modelPayload `json:"models"`
}

type modelPayload struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	ModifiedAt string `json:"modified_at"`
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
	payload, err := p.doChatRequest(ctx, requestPayload{
		Model:    p.resolveModel(req.Model),
		Messages: req.Messages,
		Stream:   false,
	})
	if err != nil {
		return provider.ChatCompletionResponse{}, err
	}

	created := parseTimestamp(payload.CreatedAt)
	model := payload.Model
	if strings.TrimSpace(model) == "" {
		model = p.resolveModel(req.Model)
	}

	return provider.ChatCompletionResponse{
		ID:      completionID(created),
		Object:  "chat.completion",
		Created: created.Unix(),
		Model:   model,
		Choices: []provider.ChatChoice{{
			Index:        0,
			Message:      payload.Message,
			FinishReason: finishReason(payload.DoneReason, payload.Done),
		}},
		Usage: provider.Usage{
			PromptTokens:     payload.PromptEvalCount,
			CompletionTokens: payload.EvalCount,
			TotalTokens:      payload.PromptEvalCount + payload.EvalCount,
		},
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

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			var payload responsePayload
			if err := json.Unmarshal([]byte(line), &payload); err != nil {
				_ = failAttempt(ctx, attemptHandle, p.resolveModel(req.Model))
				publishStreamError(ctx, events, err)
				return
			}
			if payload.Error != "" {
				_ = failAttempt(ctx, attemptHandle, p.resolveModel(req.Model))
				publishStreamError(ctx, events, errors.New(payload.Error))
				return
			}

			if payload.Done && payload.Message.Role == "" && payload.Message.Content == "" && payload.DoneReason == "" {
				return
			}

			created := parseTimestamp(payload.CreatedAt)
			model := payload.Model
			if strings.TrimSpace(model) == "" {
				model = p.resolveModel(req.Model)
			}
			event := provider.ChatCompletionStreamEvent{
				Chunk: provider.ChatCompletionChunk{
					ID:      completionID(created),
					Object:  "chat.completion.chunk",
					Created: created.Unix(),
					Model:   model,
					Choices: []provider.ChatChoice{{
						Index:        0,
						Message:      payload.Message,
						FinishReason: finishReason(payload.DoneReason, payload.Done),
					}},
				},
			}

			select {
			case <-ctx.Done():
				_ = failAttempt(ctx, attemptHandle, model)
				publishStreamError(ctx, events, ctx.Err())
				return
			case events <- event:
			}

			if payload.Done {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			_ = failAttempt(ctx, attemptHandle, p.resolveModel(req.Model))
			publishStreamError(ctx, events, err)
		}
	}()

	return events, nil
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

	models := make([]provider.Model, 0, len(payload.Models))
	for _, model := range payload.Models {
		id := strings.TrimSpace(model.Model)
		if id == "" {
			id = strings.TrimSpace(model.Name)
		}
		if id == "" {
			continue
		}

		models = append(models, provider.Model{
			ID:      id,
			Object:  "model",
			Created: parseTimestamp(model.ModifiedAt).Unix(),
			OwnedBy: "ollama",
		})
	}
	return models, nil
}

func (p Provider) doChatRequest(ctx context.Context, payload requestPayload) (responsePayload, error) {
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
		PromptTokens: provider.EstimatePromptTokens(payload.Messages),
		CreatedAt:    time.Now(),
	}

	resp, attemptHandle, err := p.doRequest(ctx, attemptMetadata, http.MethodPost, p.chatEndpointURL(), reqBody, "")
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
	if result.Error != "" {
		if completeErr := failAttempt(ctx, attemptHandle, payload.Model); completeErr != nil {
			return responsePayload{}, completeErr
		}
		return responsePayload{}, errors.New(result.Error)
	}
	if attemptHandle != nil {
		if err := attemptHandle.Complete(ctx, provider.AttemptResult{
			Model:            result.Model,
			Status:           "succeeded",
			PromptTokens:     result.PromptEvalCount,
			CompletionTokens: result.EvalCount,
			TotalTokens:      result.PromptEvalCount + result.EvalCount,
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

	resp, attemptHandle, err := p.doRequest(ctx, attemptMetadata, http.MethodPost, p.chatEndpointURL(), reqBody, "application/x-ndjson")
	if err != nil {
		return nil, nil, err
	}
	return resp, attemptHandle, nil
}

func (p Provider) chatEndpointURL() string {
	return p.baseURL + "/api/chat"
}

func (p Provider) modelsEndpointURL() string {
	return p.baseURL + "/api/tags"
}

func (p Provider) resolveModel(model string) string {
	model = strings.TrimSpace(model)
	if model != "" {
		return model
	}
	return p.defaultModel
}

func completionID(created time.Time) string {
	return fmt.Sprintf("ollama-%d", created.UnixNano())
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

func finishReason(reason string, done bool) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed != "" {
		return trimmed
	}
	if done {
		return "stop"
	}
	return ""
}

func readHTTPError(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && strings.TrimSpace(payload.Error) != "" {
		return fmt.Errorf("upstream status %d: %s", resp.StatusCode, payload.Error)
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
		return ctx, func() {}
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

func publishStreamError(ctx context.Context, events chan<- provider.ChatCompletionStreamEvent, err error) {
	if err == nil {
		return
	}

	select {
	case <-ctx.Done():
	case events <- provider.ChatCompletionStreamEvent{Err: err}:
	}
}
