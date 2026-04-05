package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/provider/router"
)

func TestRegistryServeHTTP(t *testing.T) {
	registry := NewRegistry()
	registry.RecordHTTPRequest(http.MethodGet, "/healthz", http.StatusOK)
	registry.RecordHTTPRequest(http.MethodPost, "/v1/chat/completions", http.StatusUnauthorized)
	registry.RecordReadyzFailure()
	registry.RecordGovernanceRejection("rate_limit_exceeded")
	registry.RecordStreamRequest(25 * time.Millisecond)
	registry.RecordStreamChunk()
	registry.RecordStreamChunk()
	registry.OnEvent(router.Event{
		Type:      "provider_request_failed",
		Operation: "create",
		Backend:   "mock-primary",
		Duration:  15 * time.Millisecond,
	})
	registry.OnEvent(router.Event{
		Type:      "provider_fallback_succeeded",
		Operation: "create",
		Backend:   "mock-secondary",
		Duration:  5 * time.Millisecond,
	})
	registry.OnEvent(router.Event{
		Type:      "provider_probe_succeeded",
		Operation: "probe",
		Backend:   "mock-secondary",
		Duration:  2 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	registry.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `lag_http_requests_total{method="GET",path="/healthz",status="200"} 1`) {
		t.Fatalf("expected healthz request metric, got %s", body)
	}
	if !strings.Contains(body, `lag_provider_events_total{type="provider_fallback_succeeded",operation="create",backend="mock-secondary"} 1`) {
		t.Fatalf("expected fallback metric, got %s", body)
	}
	if !strings.Contains(body, `lag_provider_operation_duration_milliseconds_count{operation="create",backend="mock-primary",result="error"} 1`) {
		t.Fatalf("expected provider duration count, got %s", body)
	}
	if !strings.Contains(body, `lag_provider_probe_results_total{backend="mock-secondary",result="success"} 1`) {
		t.Fatalf("expected provider probe result metric, got %s", body)
	}
	if !strings.Contains(body, "lag_readyz_failures_total 1") {
		t.Fatalf("expected readyz failure metric, got %s", body)
	}
	if !strings.Contains(body, `lag_governance_rejections_total{reason="rate_limit_exceeded"} 1`) {
		t.Fatalf("expected governance rejection metric, got %s", body)
	}
	if !strings.Contains(body, "lag_stream_requests_total 1") {
		t.Fatalf("expected stream request metric, got %s", body)
	}
	if !strings.Contains(body, "lag_stream_chunks_total 2") {
		t.Fatalf("expected stream chunk metric, got %s", body)
	}
	if !strings.Contains(body, "lag_stream_ttft_milliseconds_count 1") {
		t.Fatalf("expected stream ttft count metric, got %s", body)
	}
}
