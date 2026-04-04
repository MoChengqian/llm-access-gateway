package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/auth"
	providermock "github.com/MoChengqian/llm-access-gateway/internal/provider/mock"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
	"github.com/MoChengqian/llm-access-gateway/internal/service/governance"
	"go.uber.org/zap"
)

func TestHealthz(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil)

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
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestChatCompletions(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil)

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
	req.Header.Set("Authorization", "Bearer live-key")
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
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil)

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
	req.Header.Set("Authorization", "Bearer live-key")
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

func TestChatCompletionsRejectsMissingAPIKey(t *testing.T) {
	router := newTestRouter(stubAuthStore{}, nil)

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

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("expected WWW-Authenticate Bearer, got %q", got)
	}

	if bodyText := rec.Body.String(); !strings.Contains(bodyText, "\"error\":\"missing api key\"") {
		t.Fatalf("expected missing api key error, got %s", bodyText)
	}
}

func TestChatCompletionsRejectsInvalidAPIKey(t *testing.T) {
	router := newTestRouter(stubAuthStore{err: auth.ErrAPIKeyNotFound}, nil)

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
	req.Header.Set("Authorization", "Bearer bad-key")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("expected WWW-Authenticate Bearer, got %q", got)
	}

	if bodyText := rec.Body.String(); !strings.Contains(bodyText, "\"error\":\"invalid api key\"") {
		t.Fatalf("expected invalid api key error, got %s", bodyText)
	}
}

func TestChatCompletionsRejectsDisabledAPIKey(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: false,
			TenantEnabled: true,
		},
	}, nil)

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
	req.Header.Set("Authorization", "Bearer disabled-key")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}

	if bodyText := rec.Body.String(); !strings.Contains(bodyText, "\"error\":\"disabled api key\"") {
		t.Fatalf("expected disabled api key error, got %s", bodyText)
	}
}

func TestChatCompletionsRejectsRateLimitExceeded(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme", RPMLimit: 1},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, &stubGovernanceStore{count: 1, insertID: 10})

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
	req.Header.Set("Authorization", "Bearer live-key")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, rec.Code)
	}

	if bodyText := rec.Body.String(); !strings.Contains(bodyText, "\"error\":\"rate limit exceeded\"") {
		t.Fatalf("expected rate limit exceeded error, got %s", bodyText)
	}
}

func newTestRouter(store stubAuthStore, governanceStore *stubGovernanceStore) http.Handler {
	if governanceStore == nil {
		governanceStore = &stubGovernanceStore{insertID: 1}
	}

	authService := auth.NewService(store)
	governanceService := governance.NewService(governanceStore)
	chatService := chat.NewService("gpt-4o-mini", providermock.New())
	return NewRouter(zap.NewNop(), chatService, authService, governanceService)
}

type stubAuthStore struct {
	record auth.APIKeyRecord
	err    error
}

func (s stubAuthStore) LookupAPIKey(_ context.Context, _ string) (auth.APIKeyRecord, error) {
	if s.err != nil {
		return auth.APIKeyRecord{}, s.err
	}

	return s.record, nil
}

type stubGovernanceStore struct {
	count    int
	insertID uint64
	inserted governance.UsageRecord
	updated  governance.UsageUpdate
	err      error
}

func (s *stubGovernanceStore) CountRequestsSince(context.Context, uint64, time.Time) (int, error) {
	return s.count, s.err
}

func (s *stubGovernanceStore) InsertUsageRecord(_ context.Context, record governance.UsageRecord) (uint64, error) {
	s.inserted = record
	if s.err != nil {
		return 0, s.err
	}
	return s.insertID, nil
}

func (s *stubGovernanceStore) UpdateUsageRecord(_ context.Context, update governance.UsageUpdate) error {
	s.updated = update
	return s.err
}
