package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		_, _ = w.Write([]byte(`{"model":"llama3.1:8b","created_at":"2026-04-09T09:00:00Z","message":{"role":"assistant","content":"hello"},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":1}`))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		APIKey:       "test-key",
		DefaultModel: "llama3.1:8b",
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
	if payload.Model != "llama3.1:8b" || payload.Stream {
		t.Fatalf("unexpected request payload %#v", payload)
	}
	if resp.Model != "llama3.1:8b" || resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("unexpected response %#v", resp)
	}
	if resp.Usage.TotalTokens != 4 {
		t.Fatalf("unexpected usage %#v", resp.Usage)
	}
}

func TestStreamChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("{\"model\":\"llama3.1:8b\",\"created_at\":\"2026-04-09T09:00:00Z\",\"message\":{\"role\":\"assistant\",\"content\":\"hel\"},\"done\":false}\n"))
		_, _ = w.Write([]byte("{\"model\":\"llama3.1:8b\",\"created_at\":\"2026-04-09T09:00:01Z\",\"message\":{\"content\":\"lo\"},\"done\":false}\n"))
		_, _ = w.Write([]byte("{\"model\":\"llama3.1:8b\",\"created_at\":\"2026-04-09T09:00:02Z\",\"message\":{},\"done\":true,\"done_reason\":\"stop\"}\n"))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: "llama3.1:8b",
	})

	chunks, err := p.StreamChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("stream chat completion: %v", err)
	}

	var parts []string
	finishReasons := make([]string, 0, 3)
	for event := range chunks {
		if event.Err != nil {
			t.Fatalf("expected no stream error, got %v", event.Err)
		}
		parts = append(parts, event.Chunk.Choices[0].Message.Content)
		finishReasons = append(finishReasons, event.Chunk.Choices[0].FinishReason)
	}

	if got := strings.Join(parts, ""); got != "hello" {
		t.Fatalf("expected hello, got %s", got)
	}
	if finishReasons[len(finishReasons)-1] != "stop" {
		t.Fatalf("expected final stop signal, got %#v", finishReasons)
	}
}

func TestListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Fatalf("expected /api/tags, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"llama3.1:8b","model":"llama3.1:8b","modified_at":"2026-04-09T09:00:00Z"}]}`))
	}))
	defer server.Close()

	p := New(Config{BaseURL: server.URL})

	models, err := p.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if len(models) != 1 || models[0].ID != "llama3.1:8b" || models[0].OwnedBy != "ollama" {
		t.Fatalf("unexpected models %#v", models)
	}
}

func TestCreateChatCompletionRetriesRetryableStatus(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"temporary failure"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"llama3.1:8b","created_at":"2026-04-09T09:00:00Z","message":{"role":"assistant","content":"hello"},"done":true,"done_reason":"stop","prompt_eval_count":3,"eval_count":1}`))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: "llama3.1:8b",
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

func TestCreateChatCompletionHonorsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"llama3.1:8b","created_at":"2026-04-09T09:00:00Z","message":{"role":"assistant","content":"hello"},"done":true}`))
	}))
	defer server.Close()

	p := New(Config{
		BaseURL:      server.URL,
		DefaultModel: "llama3.1:8b",
		Timeout:      10 * time.Millisecond,
	})

	_, err := p.CreateChatCompletion(context.Background(), provider.ChatCompletionRequest{
		Messages: []provider.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
