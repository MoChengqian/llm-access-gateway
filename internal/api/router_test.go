package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	providermock "github.com/MoChengqian/llm-access-gateway/internal/provider/mock"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
	"go.uber.org/zap"
)

func TestHealthz(t *testing.T) {
	router := NewRouter(zap.NewNop(), chat.NewService("gpt-4o-mini", providermock.New()))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if got := rec.Header().Get("X-Request-Id"); got == "" {
		t.Fatal("expected X-Request-Id header to be set")
	}
}

func TestReadyz(t *testing.T) {
	router := NewRouter(zap.NewNop(), chat.NewService("gpt-4o-mini", providermock.New()))

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestChatCompletions(t *testing.T) {
	router := NewRouter(zap.NewNop(), chat.NewService("gpt-4o-mini", providermock.New()))

	body, err := json.Marshal(map[string]any{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": "hello",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var resp chat.CompletionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Object != "chat.completion" {
		t.Fatalf("expected object chat.completion, got %s", resp.Object)
	}

	if resp.Model != "gpt-4o-mini" {
		t.Fatalf("expected default model gpt-4o-mini, got %s", resp.Model)
	}

	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content == "" {
		t.Fatal("expected a mock completion choice in response")
	}
}

func TestChatCompletionsStream(t *testing.T) {
	router := NewRouter(zap.NewNop(), chat.NewService("gpt-4o-mini", providermock.New()))

	body, err := json.Marshal(map[string]any{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": "hello",
			},
		},
		"stream": true,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected content-type text/event-stream, got %s", got)
	}

	if !rec.Flushed {
		t.Fatal("expected streaming response to flush")
	}

	bodyText := rec.Body.String()
	dataEvents := strings.Count(bodyText, "data: ")
	if dataEvents < 2 {
		t.Fatalf("expected multiple SSE data events, got %d in %s", dataEvents, bodyText)
	}

	if !strings.Contains(bodyText, "\"object\":\"chat.completion.chunk\"") {
		t.Fatalf("expected chunk payloads, got %s", bodyText)
	}

	if !strings.Contains(bodyText, "\"delta\":{\"role\":\"assistant\",\"content\":\"This is \"}") {
		t.Fatalf("expected OpenAI-style delta payloads, got %s", bodyText)
	}

	if !strings.Contains(bodyText, "data: [DONE]") {
		t.Fatalf("expected final DONE marker, got %s", bodyText)
	}
}
