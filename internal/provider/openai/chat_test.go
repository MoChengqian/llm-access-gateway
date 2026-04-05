package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
)

func TestCreateChatCompletion(t *testing.T) {
	var authHeader string
	var payload requestPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","created":123,"model":"gpt-4.1-mini","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		APIKey:       "test-key",
		DefaultModel: "gpt-4.1-mini",
	})

	resp, err := p.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("create chat completion: %v", err)
	}

	if authHeader != "Bearer test-key" {
		t.Fatalf("expected authorization header, got %s", authHeader)
	}
	if payload.Model != "gpt-4.1-mini" || payload.Stream {
		t.Fatalf("unexpected request payload %#v", payload)
	}
	if resp.Model != "gpt-4.1-mini" || resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("unexpected response %#v", resp)
	}
}

func TestStreamChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":123,\"model\":\"gpt-4.1-mini\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hel\"},\"finish_reason\":\"\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":123,\"model\":\"gpt-4.1-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: "gpt-4.1-mini",
	})

	chunks, err := p.StreamChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("stream chat completion: %v", err)
	}

	var parts []string
	for chunk := range chunks {
		parts = append(parts, chunk.Choices[0].Message.Content)
	}

	if got := strings.Join(parts, ""); got != "hello" {
		t.Fatalf("expected hello, got %s", got)
	}
}

func TestCreateChatCompletionReturnsUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer server.Close()

	p := New(Config{BaseURL: server.URL, DefaultModel: "gpt-4.1-mini"})

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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4.1-mini","object":"model","created":123,"owned_by":"openai"}]}`))
	}))
	defer server.Close()

	p := New(Config{BaseURL: server.URL, APIKey: "test-key"})

	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "gpt-4.1-mini" {
		t.Fatalf("unexpected models %#v", models)
	}
}
