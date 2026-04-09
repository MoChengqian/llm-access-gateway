package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBenchmarkSummarySuccess(t *testing.T) {
	t.Parallel()

	expectation := benchmarkExpectation{
		Name:            "adapter-stream",
		ExpectStream:    true,
		MinSuccessRate:  1.0,
		MaxLatencyP95MS: 400,
		MaxTTFTP95MS:    120,
	}
	summary := benchmarkSummary{
		Requests:     50,
		Stream:       true,
		Success:      50,
		Failure:      0,
		StatusCounts: map[int]int{200: 50},
		LatencyP95MS: 98,
		TTFTP95MS:    28,
		StreamChunks: 150,
	}

	findings := validateBenchmarkSummary(expectation, summary)
	if len(findings) != 0 {
		t.Fatalf("validateBenchmarkSummary() findings = %v, want none", findings)
	}
}

func TestValidateBenchmarkSummaryFailure(t *testing.T) {
	t.Parallel()

	expectation := benchmarkExpectation{
		Name:            "mock-non-stream",
		ExpectStream:    false,
		MinSuccessRate:  1.0,
		MaxLatencyP95MS: 150,
	}
	summary := benchmarkSummary{
		Requests:     100,
		Stream:       false,
		Success:      95,
		Failure:      5,
		StatusCounts: map[int]int{200: 94},
		LatencyP95MS: 220,
		SampleError:  "timeout",
	}

	findings := validateBenchmarkSummary(expectation, summary)
	joined := strings.Join(findings, "\n")
	for _, needle := range []string{
		"success rate",
		"reported 5 failed requests",
		`sample_error="timeout"`,
		"status_counts[200]=94",
		"latency_p95_ms=220 exceeded 150",
	} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("validateBenchmarkSummary() findings missing %q in %q", needle, joined)
		}
	}
}

func TestValidateStreamFailureModeSuccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config{
		PrechunkOutputPath: writeTempFile(t, dir, "prechunk.out", "HTTP/1.1 200 OK\ndata: {\"id\":\"chatcmpl-mock\"}\ndata: [DONE]\n"),
		PrechunkMetricsPath: writeTempFile(t, dir, "prechunk.prom", strings.Join([]string{
			`# HELP lag_provider_events_total Total provider routing events.`,
			`lag_provider_events_total{type="provider_request_failed",operation="stream",backend="openai-primary"} 1`,
			`lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="secondary"} 1`,
			"",
		}, "\n")),
		PartialOutputPath: writeTempFile(t, dir, "partial.out", "HTTP/1.1 200 OK\ndata: {\"content\":\"partial \"}\n"),
		PartialMetricsPath: writeTempFile(t, dir, "partial.prom", strings.Join([]string{
			`lag_provider_events_total{type="provider_stream_interrupted",operation="stream",backend="openai-primary"} 1`,
			"",
		}, "\n")),
	}

	findings := validateStreamFailureMode(cfg)
	if len(findings) != 0 {
		t.Fatalf("validateStreamFailureMode() findings = %v, want none", findings)
	}
}

func TestValidateStreamFailureModeFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config{
		PrechunkOutputPath:  writeTempFile(t, dir, "prechunk.out", "HTTP/1.1 500 Internal Server Error\n"),
		PrechunkMetricsPath: writeTempFile(t, dir, "prechunk.prom", ""),
		PartialOutputPath:   writeTempFile(t, dir, "partial.out", "HTTP/1.1 200 OK\ndata: partial \ndata: [DONE]\n"),
		PartialMetricsPath: writeTempFile(t, dir, "partial.prom", strings.Join([]string{
			`lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="secondary"} 1`,
			"",
		}, "\n")),
	}

	findings := validateStreamFailureMode(cfg)
	joined := strings.Join(findings, "\n")
	for _, needle := range []string{
		`prechunk output missing "HTTP/1.1 200 OK"`,
		`prechunk output missing "data: [DONE]"`,
		`prechunk metrics missing metric lag_provider_events_total{type="provider_request_failed",operation="stream",backend="openai-primary"}`,
		`partial output unexpectedly contained "data: [DONE]"`,
		`partial metrics missing metric lag_provider_events_total{type="provider_stream_interrupted",operation="stream",backend="openai-primary"}`,
		`partial metrics metric lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="secondary"}=1, want absent or 0`,
	} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("validateStreamFailureMode() findings missing %q in %q", needle, joined)
		}
	}
}

func TestValidateAnthropicAdapterModeSuccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config{
		SystemOutputPath: writeTempFile(t, dir, "system.out", "HTTP/1.1 200 OK\n{\"choices\":[{\"message\":{\"content\":\"system=Be concise.\\\\n\\\\nUse JSON only.;messages=1;first_role=user\"}}]}\n"),
		UpstreamRequestPath: writeTempFile(t, dir, "upstream-request.json", `{
  "path": "/v1/messages",
  "headers": {
    "x-api-key": "sk-ant-test",
    "anthropic-version": "2023-06-01"
  },
  "payload": {
    "model": "claude-3-5-sonnet-latest",
    "max_tokens": 1024,
    "system": "Be concise.\n\nUse JSON only.",
    "messages": [
      {
        "role": "user",
        "content": "reply in five words"
      }
    ]
  }
}`),
		PrechunkOutputPath: writeTempFile(t, dir, "prechunk.out", "HTTP/1.1 200 OK\ndata: {\"id\":\"chatcmpl-mock\"}\ndata: [DONE]\n"),
		PrechunkMetricsPath: writeTempFile(t, dir, "prechunk.prom", strings.Join([]string{
			`lag_provider_events_total{type="provider_request_failed",operation="stream",backend="anthropic-primary"} 1`,
			`lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="secondary"} 1`,
			"",
		}, "\n")),
		PartialOutputPath: writeTempFile(t, dir, "partial.out", "HTTP/1.1 200 OK\ndata: {\"content\":\"anthropic partial \"}\n"),
		PartialMetricsPath: writeTempFile(t, dir, "partial.prom", strings.Join([]string{
			`lag_provider_events_total{type="provider_stream_interrupted",operation="stream",backend="anthropic-primary"} 1`,
			"",
		}, "\n")),
		PrimaryBackendName:   "anthropic-primary",
		SecondaryBackendName: "secondary",
		PartialOutputNeedle:  "anthropic partial ",
	}

	findings := validateAnthropicAdapterMode(cfg)
	if len(findings) != 0 {
		t.Fatalf("validateAnthropicAdapterMode() findings = %v, want none", findings)
	}
}

func TestValidateAnthropicAdapterModeFailure(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config{
		SystemOutputPath: writeTempFile(t, dir, "system.out", "HTTP/1.1 500 Internal Server Error\n"),
		UpstreamRequestPath: writeTempFile(t, dir, "upstream-request.json", `{
  "path": "/wrong",
  "headers": {},
  "payload": {
    "max_tokens": 0,
    "system": "",
    "messages": [
      {"role": "system", "content": "Be concise."},
      {"role": "assistant", "content": "bad"}
    ]
  }
}`),
		PrechunkOutputPath:  writeTempFile(t, dir, "prechunk.out", "HTTP/1.1 500 Internal Server Error\n"),
		PrechunkMetricsPath: writeTempFile(t, dir, "prechunk.prom", ""),
		PartialOutputPath:   writeTempFile(t, dir, "partial.out", "HTTP/1.1 200 OK\ndata: anthropic partial \ndata: [DONE]\n"),
		PartialMetricsPath: writeTempFile(t, dir, "partial.prom", strings.Join([]string{
			`lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="secondary"} 1`,
			"",
		}, "\n")),
		PrimaryBackendName:   "anthropic-primary",
		SecondaryBackendName: "secondary",
		PartialOutputNeedle:  "anthropic partial ",
	}

	findings := validateAnthropicAdapterMode(cfg)
	joined := strings.Join(findings, "\n")
	for _, needle := range []string{
		`system output missing "HTTP/1.1 200 OK"`,
		`system output missing "messages=1;first_role=user"`,
		`upstream request path="/wrong", want /v1/messages`,
		`upstream request missing anthropic-version header`,
		`upstream request missing x-api-key header`,
		`upstream request system="", want "Be concise.\n\nUse JSON only."`,
		`upstream request max_tokens=0, want > 0`,
		`upstream request messages=2, want 1`,
		`upstream request unexpectedly forwarded a system role inside messages`,
		`prechunk metrics missing metric lag_provider_events_total{type="provider_request_failed",operation="stream",backend="anthropic-primary"}`,
		`partial output unexpectedly contained "data: [DONE]"`,
		`partial metrics missing metric lag_provider_events_total{type="provider_stream_interrupted",operation="stream",backend="anthropic-primary"}`,
	} {
		if !strings.Contains(joined, needle) {
			t.Fatalf("validateAnthropicAdapterMode() findings missing %q in %q", needle, joined)
		}
	}
}

func writeTempFile(t *testing.T, dir string, name string, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}
