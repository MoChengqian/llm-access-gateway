package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
	"go.uber.org/zap"
)

func TestHealthz(t *testing.T) {
	router := NewRouter(zap.NewNop(), chat.NewMockService("gpt-4o-mini"))

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
	router := NewRouter(zap.NewNop(), chat.NewMockService("gpt-4o-mini"))

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestChatCompletions(t *testing.T) {
	router := NewRouter(zap.NewNop(), chat.NewMockService("gpt-4o-mini"))

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
