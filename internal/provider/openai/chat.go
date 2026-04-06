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
	resp, err := p.openStream(ctx, requestPayload{
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
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					publishStreamError(ctx, events, errors.New("upstream stream ended before [DONE]"))
					return
				}
				publishStreamError(ctx, events, err)
				return
			}

			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			if data == "[DONE]" {
				return
			}

			var payload responsePayload
			if err := json.Unmarshal([]byte(data), &payload); err != nil {
				publishStreamError(ctx, events, err)
				return
			}
			if payload.Error != nil && payload.Error.Message != "" {
				publishStreamError(ctx, events, errors.New(payload.Error.Message))
				return
			}

			select {
			case <-ctx.Done():
				publishStreamError(ctx, events, ctx.Err())
				return
			case events <- provider.ChatCompletionStreamEvent{
				Chunk: provider.ChatCompletionChunk{
					ID:      payload.ID,
					Object:  payload.Object,
					Created: payload.Created,
					Model:   payload.Model,
					Choices: toProviderStreamChoices(payload.Choices),
				},
			}:
			}
		}
	}()

	return events, nil
}

func (p Provider) ListModels(ctx context.Context) ([]provider.Model, error) {
	ctx, cancel := p.withTimeout(ctx)
	defer cancel()

	resp, err := p.doRequest(ctx, http.MethodGet, p.modelsEndpointURL(), nil, "")
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

	resp, err := p.doRequest(ctx, http.MethodPost, p.endpointURL(), reqBody, accept)
	if err != nil {
		return responsePayload{}, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	var result responsePayload
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return responsePayload{}, err
	}
	return result, nil
}

func (p Provider) openStream(ctx context.Context, payload requestPayload) (*http.Response, error) {
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return p.doRequest(ctx, http.MethodPost, p.endpointURL(), reqBody, "text/event-stream")
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

func (p Provider) doRequest(ctx context.Context, method string, url string, body []byte, accept string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		resp, err := p.doRequestOnce(ctx, method, url, body, accept)
		if err == nil {
			return resp, nil
		}

		lastErr = err
		if attempt == p.maxRetries || !shouldRetryRequest(ctx, err) {
			return nil, err
		}
		if err := p.waitRetry(ctx, attempt); err != nil {
			return nil, lastErr
		}
	}

	return nil, lastErr
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

func publishStreamError(ctx context.Context, events chan<- provider.ChatCompletionStreamEvent, err error) {
	if err == nil {
		return
	}

	select {
	case <-ctx.Done():
	case events <- provider.ChatCompletionStreamEvent{Err: err}:
	}
}
