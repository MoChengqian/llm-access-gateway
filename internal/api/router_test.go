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

	"github.com/MoChengqian/llm-access-gateway/internal/api/handlers"
	"github.com/MoChengqian/llm-access-gateway/internal/auth"
	"github.com/MoChengqian/llm-access-gateway/internal/obs/metrics"
	providermock "github.com/MoChengqian/llm-access-gateway/internal/provider/mock"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
	"github.com/MoChengqian/llm-access-gateway/internal/service/governance"
	modelsservice "github.com/MoChengqian/llm-access-gateway/internal/service/models"
	usageservice "github.com/MoChengqian/llm-access-gateway/internal/service/usage"
	"go.uber.org/zap"
)

func TestHealthz(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if got := rec.Header().Get("X-Request-Id"); got == "" {
		t.Fatal("expected X-Request-Id header to be set")
	}
	if got := rec.Header().Get("X-Trace-Id"); got == "" {
		t.Fatal("expected X-Trace-Id header to be set")
	}
	if rec.Header().Get("X-Trace-Id") != rec.Header().Get("X-Request-Id") {
		t.Fatalf("expected X-Trace-Id to match X-Request-Id, got trace=%s request=%s", rec.Header().Get("X-Trace-Id"), rec.Header().Get("X-Request-Id"))
	}
}

func TestReadyz(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestReadyzReturnsServiceUnavailableWhenProvidersUnready(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil, nil, providerHealthStub{
		ready: false,
		statuses: []handlers.ProviderBackendStatus{
			{Name: "mock-primary", Healthy: false, ConsecutiveFailures: 1},
			{Name: "mock-secondary", Healthy: false, ConsecutiveFailures: 1},
		},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestDebugProviders(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil, nil, providerHealthStub{
		ready: true,
		statuses: []handlers.ProviderBackendStatus{
			{Name: "mock-primary", Healthy: false, ConsecutiveFailures: 1},
			{Name: "mock-secondary", Healthy: true, ConsecutiveFailures: 0},
		},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/debug/providers", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	bodyText := rec.Body.String()
	if !strings.Contains(bodyText, "\"ready\":true") || !strings.Contains(bodyText, "\"mock-primary\"") {
		t.Fatalf("expected provider status payload, got %s", bodyText)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	registry := metrics.NewRegistry()
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil, nil, providerHealthStub{ready: true}, registry)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	bodyText := rec.Body.String()
	if !strings.Contains(bodyText, `lag_http_requests_total{method="GET",path="/healthz",status="200"} 1`) {
		t.Fatalf("expected healthz metric, got %s", bodyText)
	}
}

func TestMetricsCountsReadyzFailure(t *testing.T) {
	registry := metrics.NewRegistry()
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil, nil, providerHealthStub{ready: false}, registry)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "lag_readyz_failures_total 1") {
		t.Fatalf("expected readyz failure metric, got %s", rec.Body.String())
	}
}

func TestModelsList(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer live-key")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	bodyText := rec.Body.String()
	if !strings.Contains(bodyText, `"object":"list"`) || !strings.Contains(bodyText, `"id":"gpt-4o-mini"`) {
		t.Fatalf("expected models payload, got %s", bodyText)
	}
}

func TestModelsListRejectsMissingAPIKey(t *testing.T) {
	router := newTestRouter(stubAuthStore{}, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestUsageReturnsTenantSummaryAndRecentRecords(t *testing.T) {
	governanceStore := &stubGovernanceStore{
		insertID:           1,
		tokensTotal:        140,
		requestsLastMinute: 2,
		tokensLastMinute:   42,
		recentUsageRecords: []usageservice.RecentUsageRecord{
			{
				RequestID:        "req-1",
				APIKeyID:         10,
				Model:            "gpt-4o-mini",
				Stream:           true,
				Status:           "succeeded",
				PromptTokens:     10,
				CompletionTokens: 8,
				TotalTokens:      18,
			},
		},
	}
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme", RPMLimit: 60, TPMLimit: 4000, TokenBudget: 1000},
			APIKeyEnabled: true,
			TenantEnabled: true,
			APIKeyID:      10,
		},
	}, governanceStore, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/usage?limit=5", nil)
	req.Header.Set("Authorization", "Bearer live-key")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	bodyText := rec.Body.String()
	if !strings.Contains(bodyText, `"object":"usage"`) || !strings.Contains(bodyText, `"requests_last_minute":2`) {
		t.Fatalf("expected usage summary payload, got %s", bodyText)
	}
	if !strings.Contains(bodyText, `"remaining_token_budget":860`) || !strings.Contains(bodyText, `"request_id":"req-1"`) {
		t.Fatalf("expected recent usage payload, got %s", bodyText)
	}
	if governanceStore.lastRecentUsageLimit != 5 {
		t.Fatalf("expected recent usage limit 5, got %d", governanceStore.lastRecentUsageLimit)
	}
}

func TestUsageRejectsMissingAPIKey(t *testing.T) {
	router := newTestRouter(stubAuthStore{}, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/usage", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
	if bodyText := rec.Body.String(); !strings.Contains(bodyText, `"error":"missing api key"`) {
		t.Fatalf("expected missing api key error, got %s", bodyText)
	}
}

func TestUsageRejectsInvalidLimit(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/usage?limit=bad", nil)
	req.Header.Set("Authorization", "Bearer live-key")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
	if bodyText := rec.Body.String(); !strings.Contains(bodyText, `"error":"invalid limit"`) {
		t.Fatalf("expected invalid limit error, got %s", bodyText)
	}
}

func TestChatCompletions(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil, nil, nil, nil)

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
	registry := metrics.NewRegistry()
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil, nil, nil, registry)

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

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	bodyText = rec.Body.String()
	if !strings.Contains(bodyText, "lag_stream_requests_total 1") {
		t.Fatalf("expected stream request metric, got %s", bodyText)
	}
	if !strings.Contains(bodyText, "lag_stream_chunks_total 4") {
		t.Fatalf("expected stream chunk metric, got %s", bodyText)
	}
	if !strings.Contains(bodyText, "lag_stream_ttft_milliseconds_count 1") {
		t.Fatalf("expected stream ttft metric, got %s", bodyText)
	}
}

func TestChatCompletionsRejectsMissingAPIKey(t *testing.T) {
	router := newTestRouter(stubAuthStore{}, nil, nil, nil, nil)

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

func TestChatCompletionsRejectsOversizedRequestBody(t *testing.T) {
	router := newTestRouterWithMaxBodyBytes(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, nil, nil, nil, nil, 64)

	body := `{"messages":[{"role":"user","content":"` + strings.Repeat("x", 128) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer live-key")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d", http.StatusRequestEntityTooLarge, rec.Code)
	}
	if bodyText := rec.Body.String(); !strings.Contains(bodyText, `"error":"request body too large"`) {
		t.Fatalf("expected request body too large error, got %s", bodyText)
	}
}

func TestChatCompletionsRejectsInvalidAPIKey(t *testing.T) {
	router := newTestRouter(stubAuthStore{err: auth.ErrAPIKeyNotFound}, nil, nil, nil, nil)

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
	}, nil, nil, nil, nil)

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
	registry := metrics.NewRegistry()
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme", RPMLimit: 1},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, &stubGovernanceStore{insertID: 10}, &stubLimiter{admitErr: governance.ErrRateLimitExceeded}, nil, registry)

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

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), `lag_governance_rejections_total{reason="rate_limit_exceeded"} 1`) {
		t.Fatalf("expected governance rejection metric, got %s", rec.Body.String())
	}
}

func TestChatCompletionsRejectsTokenRateLimitExceeded(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme", TPMLimit: 1},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, &stubGovernanceStore{insertID: 10}, &stubLimiter{admitErr: governance.ErrTokenLimitExceeded}, nil, nil)

	body, err := json.Marshal(map[string]any{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": "hello world",
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

	if bodyText := rec.Body.String(); !strings.Contains(bodyText, "\"error\":\"token rate limit exceeded\"") {
		t.Fatalf("expected token rate limit exceeded error, got %s", bodyText)
	}
}

func TestChatCompletionsRejectsBudgetExceeded(t *testing.T) {
	router := newTestRouter(stubAuthStore{
		record: auth.APIKeyRecord{
			Tenant:        auth.Tenant{ID: 1, Name: "acme", TokenBudget: 1},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	}, &stubGovernanceStore{tokensTotal: 1, insertID: 10}, &stubLimiter{}, nil, nil)

	body, err := json.Marshal(map[string]any{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": "hello world",
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

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, rec.Code)
	}

	if bodyText := rec.Body.String(); !strings.Contains(bodyText, "\"error\":\"budget exceeded\"") {
		t.Fatalf("expected budget exceeded error, got %s", bodyText)
	}
}

func newTestRouter(store stubAuthStore, governanceStore *stubGovernanceStore, limiter *stubLimiter, providers handlers.ProviderHealthReader, registry *metrics.Registry) http.Handler {
	return newTestRouterWithMaxBodyBytes(store, governanceStore, limiter, providers, registry, 1<<20)
}

func newTestRouterWithMaxBodyBytes(store stubAuthStore, governanceStore *stubGovernanceStore, limiter *stubLimiter, providers handlers.ProviderHealthReader, registry *metrics.Registry, maxRequestBodyBytes int64) http.Handler {
	if governanceStore == nil {
		governanceStore = &stubGovernanceStore{insertID: 1}
	}
	if limiter == nil {
		limiter = &stubLimiter{}
	}
	if registry == nil {
		registry = metrics.NewRegistry()
	}

	authService := auth.NewService(store)
	governanceService := governance.NewService(governanceStore, limiter)
	chatService := chat.NewService("gpt-4o-mini", providermock.New())
	modelsService := modelsservice.NewService([]modelsservice.Source{providermock.New()})
	usageService := usageservice.NewService(governanceStore)
	return NewRouter(zap.NewNop(), chatService, modelsService, usageService, authService, governanceService, providers, registry, registry, maxRequestBodyBytes)
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
	tokensTotal          int
	insertID             uint64
	inserted             governance.UsageRecord
	updated              governance.UsageUpdate
	err                  error
	requestsLastMinute   int
	tokensLastMinute     int
	recentUsageRecords   []usageservice.RecentUsageRecord
	lastRecentUsageLimit int
}

func (s *stubGovernanceStore) SumTotalTokens(context.Context, uint64) (int, error) {
	return s.tokensTotal, s.err
}

func (s *stubGovernanceStore) CountRequestsSince(context.Context, uint64, time.Time) (int, error) {
	return s.requestsLastMinute, s.err
}

func (s *stubGovernanceStore) SumTotalTokensSince(context.Context, uint64, time.Time) (int, error) {
	return s.tokensLastMinute, s.err
}

func (s *stubGovernanceStore) ListRecentUsageRecords(_ context.Context, _ uint64, limit int) ([]usageservice.RecentUsageRecord, error) {
	s.lastRecentUsageLimit = limit
	return s.recentUsageRecords, s.err
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

type stubLimiter struct {
	admitErr error
}

func (l *stubLimiter) Admit(context.Context, auth.Principal, int, time.Time) error {
	return l.admitErr
}

func (l *stubLimiter) RecordCompletionTokens(context.Context, auth.Principal, int, time.Time) error {
	return nil
}

type providerHealthStub struct {
	ready    bool
	statuses []handlers.ProviderBackendStatus
}

func (s providerHealthStub) Ready() bool {
	return s.ready
}

func (s providerHealthStub) BackendStatuses() []handlers.ProviderBackendStatus {
	return s.statuses
}
