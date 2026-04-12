package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/api/handlers"
	"github.com/MoChengqian/llm-access-gateway/internal/provider/router"
)

const (
	metricsMockPrimary   = "mock-primary"
	metricsMockSecondary = "mock-secondary"
)

func TestRegistryServeHTTP(t *testing.T) {
	registry := NewRegistry()
	cooldownUntil := time.Now().Add(time.Minute)
	registry.SyncProviderStatuses([]handlers.ProviderBackendStatus{
		{
			Name:                metricsMockPrimary,
			Healthy:             false,
			ConsecutiveFailures: 2,
			UnhealthyUntil:      cooldownUntil,
		},
		{
			Name:    metricsMockSecondary,
			Healthy: true,
		},
	})
	registry.RecordHTTPRequest(http.MethodGet, "/healthz", http.StatusOK, 12*time.Millisecond)
	registry.RecordHTTPRequest(http.MethodPost, "/v1/chat/completions", http.StatusUnauthorized, 34*time.Millisecond)
	registry.RecordReadyzFailure()
	registry.RecordGovernanceRejection("rate_limit_exceeded")
	registry.RecordStreamRequest(25 * time.Millisecond)
	registry.RecordStreamChunk()
	registry.RecordStreamChunk()
	registry.OnEvent(router.Event{
		Type:                "provider_request_failed",
		Operation:           "create",
		Backend:             metricsMockPrimary,
		Duration:            15 * time.Millisecond,
		ConsecutiveFailures: 2,
		UnhealthyUntil:      cooldownUntil,
	})
	registry.OnEvent(router.Event{
		Type:      "provider_fallback_succeeded",
		Operation: "create",
		Backend:   metricsMockSecondary,
		Duration:  5 * time.Millisecond,
	})
	registry.OnEvent(router.Event{
		Type:      "provider_probe_succeeded",
		Operation: "probe",
		Backend:   metricsMockSecondary,
		Duration:  2 * time.Millisecond,
	})
	registry.OnEvent(router.Event{
		Type:                "provider_stream_interrupted",
		Operation:           "stream",
		Backend:             metricsMockPrimary,
		Duration:            7 * time.Millisecond,
		ConsecutiveFailures: 2,
		UnhealthyUntil:      cooldownUntil,
	})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	registry.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	body := rec.Body.String()
	assertMetricsBodyContains(t, body,
		`lag_http_requests_total{method="GET",path="/healthz",status="200"} 1`,
		`lag_provider_events_total{type="provider_fallback_succeeded",operation="create",backend="`+metricsMockSecondary+`"} 1`,
		`lag_http_request_duration_milliseconds_count{method="GET",path="/healthz",status="200"} 1`,
		`lag_provider_operation_duration_milliseconds_count{operation="create",backend="`+metricsMockPrimary+`",result="error"} 1`,
		`lag_provider_operation_duration_milliseconds_count{operation="stream",backend="`+metricsMockPrimary+`",result="error"} 1`,
		`lag_provider_probe_results_total{backend="`+metricsMockSecondary+`",result="success"} 1`,
		`lag_provider_backend_healthy{backend="`+metricsMockPrimary+`"} 0`,
		`lag_provider_backend_consecutive_failures{backend="`+metricsMockPrimary+`"} 2`,
		`lag_provider_backend_cooldown_remaining_milliseconds{backend="`+metricsMockPrimary+`"} `,
		"lag_provider_ready 1",
		"lag_readyz_failures_total 1",
		`lag_governance_rejections_total{reason="rate_limit_exceeded"} 1`,
		"lag_stream_requests_total 1",
		"lag_stream_chunks_total 2",
		"lag_stream_ttft_milliseconds_count 1",
	)
}

func assertMetricsBodyContains(t *testing.T, body string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if strings.Contains(body, needle) {
			continue
		}
		t.Fatalf("expected metric %q, got %s", needle, body)
	}
}
