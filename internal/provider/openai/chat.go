package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
)

type Config struct {
	BaseURL      string
	APIKey       string
	DefaultModel string
	HTTPClient   *http.Client
}

type Provider struct {
	baseURL      string
	apiKey       string
	defaultModel string
	httpClient   *http.Client
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

func New(cfg Config) Provider {
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	return Provider{
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		apiKey:       strings.TrimSpace(cfg.APIKey),
		defaultModel: strings.TrimSpace(cfg.DefaultModel),
		httpClient:   client,
	}
}

func (p Provider) CreateChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (provider.ChatCompletionResponse, error) {
	payload, err := p.doRequest(ctx, requestPayload{
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

func (p Provider) StreamChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (<-chan provider.ChatCompletionChunk, error) {
	resp, err := p.openStream(ctx, requestPayload{
		Model:    p.resolveModel(req.Model),
		Messages: req.Messages,
		Stream:   true,
	})
	if err != nil {
		return nil, err
	}

	chunks := make(chan provider.ChatCompletionChunk)
	go func() {
		defer close(chunks)
		defer func() {
			_ = resp.Body.Close()
		}()

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
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
				return
			}

			chunks <- provider.ChatCompletionChunk{
				ID:      payload.ID,
				Object:  payload.Object,
				Created: payload.Created,
				Model:   payload.Model,
				Choices: toProviderStreamChoices(payload.Choices),
			}
		}
	}()

	return chunks, nil
}

func (p Provider) doRequest(ctx context.Context, payload requestPayload, stream bool) (responsePayload, error) {
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return responsePayload{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL(), bytes.NewReader(reqBody))
	if err != nil {
		return responsePayload{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return responsePayload{}, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		return responsePayload{}, readHTTPError(resp)
	}

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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpointURL(), bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		defer func() {
			_ = resp.Body.Close()
		}()
		return nil, readHTTPError(resp)
	}

	return resp, nil
}

func (p Provider) endpointURL() string {
	return p.baseURL + "/chat/completions"
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
