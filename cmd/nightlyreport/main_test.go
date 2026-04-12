package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildReport(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config{
		BaselinePath:         writeTempFile(t, dir, "baseline.json", baselineFixtureJSON),
		MockNonStreamPath:    writeTempFile(t, dir, "mock-non-stream.json", `{"requests":100,"concurrency":10,"stream":false,"success":100,"failure":0,"total_duration_ms":140,"latency_p50_ms":12,"latency_p95_ms":25,"latency_max_ms":30}`),
		MockStreamPath:       writeTempFile(t, dir, "mock-stream.json", `{"requests":50,"concurrency":5,"stream":true,"success":50,"failure":0,"total_duration_ms":100,"latency_p50_ms":8,"latency_p95_ms":20,"latency_max_ms":23,"ttft_p50_ms":4,"ttft_p95_ms":11,"ttft_max_ms":15,"stream_chunks_total":200}`),
		AdapterNonStreamPath: writeTempFile(t, dir, "adapter-non-stream.json", `{"requests":100,"concurrency":10,"stream":false,"success":100,"failure":0,"total_duration_ms":170,"latency_p50_ms":15,"latency_p95_ms":32,"latency_max_ms":40}`),
		AdapterStreamPath:    writeTempFile(t, dir, "adapter-stream.json", `{"requests":50,"concurrency":5,"stream":true,"success":50,"failure":0,"total_duration_ms":830,"latency_p50_ms":80,"latency_p95_ms":105,"latency_max_ms":110,"ttft_p50_ms":7,"ttft_p95_ms":30,"ttft_max_ms":31,"stream_chunks_total":150}`),
		PrechunkOutputPath:   writeTempFile(t, dir, "prechunk.out", "HTTP/1.1 200 OK\ndata: {\"id\":\"chatcmpl-mock\"}\ndata: [DONE]\n"),
		PrechunkMetricsPath: writeTempFile(t, dir, "prechunk.prom", strings.Join([]string{
			"lag_provider_events_total{type=\"provider_request_failed\",operation=\"stream\",backend=\"openai-primary\"} 1",
			"lag_provider_events_total{type=\"provider_fallback_succeeded\",operation=\"stream\",backend=\"secondary\"} 1",
			"",
		}, "\n")),
		PartialOutputPath:         writeTempFile(t, dir, "partial.out", "HTTP/1.1 200 OK\ndata: {\"content\":\"partial \"}\n"),
		PartialMetricsPath:        writeTempFile(t, dir, "partial.prom", "lag_provider_events_total{type=\"provider_stream_interrupted\",operation=\"stream\",backend=\"openai-primary\"} 1\n"),
		AnthropicSystemOutputPath: writeTempFile(t, dir, "anthropic-system.out", "HTTP/1.1 200 OK\n{\"choices\":[{\"message\":{\"content\":\"system=Be concise.\\\\n\\\\nUse JSON only.;messages=1;first_role=user\"}}]}\n"),
		AnthropicUpstreamRequestPath: writeTempFile(t, dir, "anthropic-upstream-request.json", `{
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
		AnthropicPrechunkOutputPath: writeTempFile(t, dir, "anthropic-prechunk.out", "HTTP/1.1 200 OK\ndata: {\"id\":\"chatcmpl-mock\"}\ndata: [DONE]\n"),
		AnthropicPrechunkMetricsPath: writeTempFile(t, dir, "anthropic-prechunk.prom", strings.Join([]string{
			"lag_provider_events_total{type=\"provider_request_failed\",operation=\"stream\",backend=\"anthropic-primary\"} 1",
			"lag_provider_events_total{type=\"provider_fallback_succeeded\",operation=\"stream\",backend=\"secondary\"} 1",
			"",
		}, "\n")),
		AnthropicPartialOutputPath:  writeTempFile(t, dir, "anthropic-partial.out", "HTTP/1.1 200 OK\ndata: {\"content\":\"anthropic partial \"}\n"),
		AnthropicPartialMetricsPath: writeTempFile(t, dir, "anthropic-partial.prom", "lag_provider_events_total{type=\"provider_stream_interrupted\",operation=\"stream\",backend=\"anthropic-primary\"} 1\n"),
	}

	report, err := buildReport(cfg)
	if err != nil {
		t.Fatalf("buildReport() error = %v", err)
	}

	for _, needle := range []string{
		"# Nightly Verification Summary",
		"Benchmark Delta Vs Persisted Baseline",
		"mock-non-stream",
		"slower",
		"adapter-non-stream",
		"faster",
		"Pre-first-chunk fallback",
		"Post-first-chunk interruption",
		"Anthropic Adapter Drill Summary",
		"System prompt translation",
		"Anthropic pre-first-chunk fallback",
		"Anthropic post-first-chunk interruption",
		"pass",
	} {
		if !strings.Contains(report, needle) {
			t.Fatalf("buildReport() missing %q in report:\n%s", needle, report)
		}
	}
}

func TestWriteReportFileCreatesParentDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outputPath := filepath.Join(root, "nested", "report", "nightly-summary.md")

	if err := writeReportFile(reportFile{path: outputPath, body: "# hello\n"}); err != nil {
		t.Fatalf("writeReportFile() error = %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", outputPath, err)
	}
	if string(data) != "# hello\n" {
		t.Fatalf("unexpected report body %q", string(data))
	}
}

const baselineFixtureJSON = `{
  "source": "docs fixtures",
  "benchmarks": {
    "mock-non-stream": {
      "requests": 100,
      "concurrency": 10,
      "stream": false,
      "success": 100,
      "failure": 0,
      "total_duration_ms": 135,
      "latency_p50_ms": 11,
      "latency_p95_ms": 22,
      "latency_max_ms": 24
    },
    "adapter-non-stream": {
      "requests": 100,
      "concurrency": 10,
      "stream": false,
      "success": 100,
      "failure": 0,
      "total_duration_ms": 164,
      "latency_p50_ms": 14,
      "latency_p95_ms": 34,
      "latency_max_ms": 37
    },
    "mock-stream": {
      "requests": 50,
      "concurrency": 5,
      "stream": true,
      "success": 50,
      "failure": 0,
      "total_duration_ms": 91,
      "latency_p50_ms": 7,
      "latency_p95_ms": 19,
      "latency_max_ms": 20,
      "ttft_p50_ms": 4,
      "ttft_p95_ms": 12,
      "ttft_max_ms": 15,
      "stream_chunks_total": 200
    },
    "adapter-stream": {
      "requests": 50,
      "concurrency": 5,
      "stream": true,
      "success": 50,
      "failure": 0,
      "total_duration_ms": 814,
      "latency_p50_ms": 79,
      "latency_p95_ms": 98,
      "latency_max_ms": 99,
      "ttft_p50_ms": 6,
      "ttft_p95_ms": 28,
      "ttft_max_ms": 30,
      "stream_chunks_total": 150
    }
  }
}`

func writeTempFile(t *testing.T, dir string, name string, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}
