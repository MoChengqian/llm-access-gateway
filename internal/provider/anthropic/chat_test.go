package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
)

const (
	anthropicTestContentTypeHeader    = "Content-Type"
	anthropicTestJSONContentType      = "application/json"
	anthropicTestEventStreamType      = "text/event-stream"
	anthropicTestDefaultModel         = "claude-3-5-sonnet-latest"
	anthropicMessageStartEvent        = "event: message_start\n"
	anthropicContentBlockDeltaEvent   = "event: content_block_delta\n"
	anthropicCompletionResponsePrefix = `{"id":"msg_123","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"model":"`
	anthropicCompletionResponseSuffix = `","stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`
)

func TestCreateChatCompletion(t *testing.T) {
	var apiKeyHeader string
	var versionHeader string
	var payload requestPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKeyHeader = r.Header.Get("x-api-key")
		versionHeader = r.Header.Get("anthropic-version")
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set(anthropicTestContentTypeHeader, anthropicTestJSONContentType)
		_, _ = w.Write([]byte(anthropicCompletionResponsePrefix + anthropicTestDefaultModel + anthropicCompletionResponseSuffix))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		APIKey:       "test-key",
		DefaultModel: anthropicTestDefaultModel,
		MaxTokens:    2048,
	})

	resp, err := p.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("create chat completion: %v", err)
	}

	if apiKeyHeader != "test-key" {
		t.Fatalf("expected x-api-key header, got %q", apiKeyHeader)
	}
	if versionHeader != defaultAPIVersion {
		t.Fatalf("expected anthropic-version header %q, got %q", defaultAPIVersion, versionHeader)
	}
	if payload.Model != anthropicTestDefaultModel || payload.MaxTokens != 2048 {
		t.Fatalf("unexpected request payload %#v", payload)
	}
	if resp.Model != anthropicTestDefaultModel || resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("unexpected response %#v", resp)
	}
	if resp.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected usage %#v", resp.Usage)
	}
}

func TestCreateChatCompletionMapsSystemMessagesToTopLevelSystemPrompt(t *testing.T) {
	var payload requestPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set(anthropicTestContentTypeHeader, anthropicTestJSONContentType)
		_, _ = w.Write([]byte(anthropicCompletionResponsePrefix + anthropicTestDefaultModel + anthropicCompletionResponseSuffix))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: anthropicTestDefaultModel,
	})

	_, err := p.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{
			{Role: "system", Content: "Be concise."},
			{Role: "user", Content: "hi"},
			{Role: "system", Content: "Use JSON only."},
		},
	})
	if err != nil {
		t.Fatalf("create chat completion: %v", err)
	}

	if payload.System != "Be concise.\n\nUse JSON only." {
		t.Fatalf("expected joined system prompt, got %q", payload.System)
	}
	if len(payload.Messages) != 1 {
		t.Fatalf("expected only non-system messages, got %#v", payload.Messages)
	}
	if payload.Messages[0].Role != "user" || payload.Messages[0].Content != "hi" {
		t.Fatalf("unexpected translated messages %#v", payload.Messages)
	}
}

func TestStreamChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(anthropicTestContentTypeHeader, anthropicTestEventStreamType)
		_, _ = w.Write([]byte(anthropicMessageStartEvent))
		_, _ = w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"" + anthropicTestDefaultModel + "\",\"content\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte(anthropicContentBlockDeltaEvent))
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hel\"}}\n\n"))
		_, _ = w.Write([]byte(anthropicContentBlockDeltaEvent))
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"lo\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: anthropicTestDefaultModel,
	})

	chunks, err := p.StreamChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("stream chat completion: %v", err)
	}

	var parts []string
	var firstRole string
	var finalFinishReason string
	for event := range chunks {
		if event.Err != nil {
			t.Fatalf("expected no stream error, got %v", event.Err)
		}
		if firstRole == "" {
			firstRole = event.Chunk.Choices[0].Message.Role
		}
		parts = append(parts, event.Chunk.Choices[0].Message.Content)
		if event.Chunk.Choices[0].FinishReason != "" {
			finalFinishReason = event.Chunk.Choices[0].FinishReason
		}
	}

	if got := strings.Join(parts, ""); got != "hello" {
		t.Fatalf("expected hello, got %s", got)
	}
	if firstRole != "assistant" {
		t.Fatalf("expected assistant role on first chunk, got %q", firstRole)
	}
	if finalFinishReason != "stop" {
		t.Fatalf("expected final stop signal, got %q", finalFinishReason)
	}
}

func TestCreateChatCompletionReturnsUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`))
	}))
	defer server.Close()

	p := New(Config{BaseURL: server.URL, DefaultModel: anthropicTestDefaultModel})

	_, err := p.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("expected upstream error, got %v", err)
	}
}

func TestListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Fatalf("expected /models, got %s", r.URL.Path)
		}
		w.Header().Set(anthropicTestContentTypeHeader, anthropicTestJSONContentType)
		_, _ = w.Write([]byte(`{"data":[{"id":"` + anthropicTestDefaultModel + `","type":"model","created_at":"2026-04-09T09:00:00Z"}]}`))
	}))
	defer server.Close()

	p := New(Config{BaseURL: server.URL})

	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 1 || models[0].ID != anthropicTestDefaultModel || models[0].OwnedBy != "anthropic" {
		t.Fatalf("unexpected models %#v", models)
	}
}

func TestCreateChatCompletionRetriesRetryableStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"temporary failure"}}`))
			return
		}

		w.Header().Set(anthropicTestContentTypeHeader, anthropicTestJSONContentType)
		_, _ = w.Write([]byte(anthropicCompletionResponsePrefix + anthropicTestDefaultModel + anthropicCompletionResponseSuffix))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: anthropicTestDefaultModel,
		MaxRetries:   1,
		RetryBackoff: time.Millisecond,
	})

	resp, err := p.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("unexpected response %#v", resp)
	}
}

func TestStreamChatCompletionRetriesBeforeFirstEvent(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"temporary failure"}}`))
			return
		}

		w.Header().Set(anthropicTestContentTypeHeader, anthropicTestEventStreamType)
		_, _ = w.Write([]byte(anthropicMessageStartEvent))
		_, _ = w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"" + anthropicTestDefaultModel + "\",\"content\":[]}}\n\n"))
		_, _ = w.Write([]byte(anthropicContentBlockDeltaEvent))
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: anthropicTestDefaultModel,
		MaxRetries:   1,
		RetryBackoff: time.Millisecond,
	})

	chunks, err := p.StreamChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}

	var content strings.Builder
	for event := range chunks {
		if event.Err != nil {
			t.Fatalf("expected no stream error, got %v", event.Err)
		}
		content.WriteString(event.Chunk.Choices[0].Message.Content)
	}

	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if content.String() != "ok" {
		t.Fatalf("expected ok, got %q", content.String())
	}
}

func TestCreateChatCompletionHonorsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set(anthropicTestContentTypeHeader, anthropicTestJSONContentType)
		_, _ = w.Write([]byte(anthropicCompletionResponsePrefix + anthropicTestDefaultModel + anthropicCompletionResponseSuffix))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: anthropicTestDefaultModel,
		Timeout:      10 * time.Millisecond,
	})

	_, err := p.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("expected deadline exceeded error, got %v", err)
	}
}

func TestStreamChatCompletionPropagatesMidstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(anthropicTestContentTypeHeader, anthropicTestEventStreamType)
		_, _ = w.Write([]byte(anthropicMessageStartEvent))
		_, _ = w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"" + anthropicTestDefaultModel + "\",\"content\":[]}}\n\n"))
		_, _ = w.Write([]byte(anthropicContentBlockDeltaEvent))
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hel\"}}\n\n"))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: anthropicTestDefaultModel,
	})

	events, err := p.StreamChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("stream chat completion: %v", err)
	}

	first, ok := <-events
	if !ok {
		t.Fatal("expected first stream event")
	}
	if first.Err != nil || first.Chunk.Choices[0].Message.Content != "hel" {
		t.Fatalf("unexpected first stream event %#v", first)
	}

	second, ok := <-events
	if !ok {
		t.Fatal("expected terminal error event")
	}
	if second.Err == nil || !strings.Contains(second.Err.Error(), "before message_stop") {
		t.Fatalf("expected terminal midstream error, got %#v", second)
	}
}
