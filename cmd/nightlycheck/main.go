package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	modeBenchmarks     = "benchmarks"
	modeStreamFailures = "stream-failures"
	modeAnthropic      = "anthropic-adapter"

	httpStatusOKLine    = "HTTP/1.1 200 OK"
	streamDoneMarker    = "data: [DONE]"
	labelErrorFormat    = "%s: %v"
	prechunkOutputLabel = "prechunk output"
	partialOutputLabel  = "partial output"

	defaultMinSuccessRate                  = 1.0
	defaultMockNonStreamMaxLatencyP95MS    = 150
	defaultMockStreamMaxLatencyP95MS       = 150
	defaultMockStreamMaxTTFTP95MS          = 75
	defaultAdapterNonStreamMaxLatencyP95MS = 250
	defaultAdapterStreamMaxLatencyP95MS    = 400
	defaultAdapterStreamMaxTTFTP95MS       = 120
)

type config struct {
	Mode string

	MockNonStreamPath    string
	MockStreamPath       string
	AdapterNonStreamPath string
	AdapterStreamPath    string
	MinSuccessRate       float64

	MockNonStreamMaxLatencyP95MS    int64
	MockStreamMaxLatencyP95MS       int64
	MockStreamMaxTTFTP95MS          int64
	AdapterNonStreamMaxLatencyP95MS int64
	AdapterStreamMaxLatencyP95MS    int64
	AdapterStreamMaxTTFTP95MS       int64

	PrechunkOutputPath  string
	PrechunkMetricsPath string
	PartialOutputPath   string
	PartialMetricsPath  string
	SystemOutputPath    string
	UpstreamRequestPath string

	PrimaryBackendName   string
	SecondaryBackendName string
	PartialOutputNeedle  string
}

type benchmarkSummary struct {
	Requests     int         `json:"requests"`
	Concurrency  int         `json:"concurrency"`
	Stream       bool        `json:"stream"`
	Success      int         `json:"success"`
	Failure      int         `json:"failure"`
	StatusCounts map[int]int `json:"status_counts"`
	LatencyP95MS int64       `json:"latency_p95_ms"`
	TTFTP95MS    int64       `json:"ttft_p95_ms,omitempty"`
	StreamChunks int         `json:"stream_chunks_total,omitempty"`
	SampleError  string      `json:"sample_error,omitempty"`
}

type benchmarkExpectation struct {
	Name            string
	Path            string
	ExpectStream    bool
	MinSuccessRate  float64
	MaxLatencyP95MS int64
	MaxTTFTP95MS    int64
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse flags: %v\n", err)
		os.Exit(1)
	}

	var findings []string
	switch cfg.Mode {
	case modeBenchmarks:
		findings = validateBenchmarkMode(cfg)
	case modeStreamFailures:
		findings = validateStreamFailureMode(cfg)
	case modeAnthropic:
		findings = validateAnthropicAdapterMode(cfg)
	default:
		fmt.Fprintf(os.Stderr, "unsupported mode %q\n", cfg.Mode)
		os.Exit(1)
	}

	if len(findings) > 0 {
		for _, finding := range findings {
			fmt.Fprintf(os.Stderr, "nightly regression: %s\n", finding)
		}
		os.Exit(1)
	}

	fmt.Printf("nightly check passed: mode=%s\n", cfg.Mode)
}

func parseFlags(args []string) (config, error) {
	cfg := config{
		Mode:                            modeBenchmarks,
		MinSuccessRate:                  defaultMinSuccessRate,
		MockNonStreamMaxLatencyP95MS:    defaultMockNonStreamMaxLatencyP95MS,
		MockStreamMaxLatencyP95MS:       defaultMockStreamMaxLatencyP95MS,
		MockStreamMaxTTFTP95MS:          defaultMockStreamMaxTTFTP95MS,
		AdapterNonStreamMaxLatencyP95MS: defaultAdapterNonStreamMaxLatencyP95MS,
		AdapterStreamMaxLatencyP95MS:    defaultAdapterStreamMaxLatencyP95MS,
		AdapterStreamMaxTTFTP95MS:       defaultAdapterStreamMaxTTFTP95MS,
		PrimaryBackendName:              "openai-primary",
		SecondaryBackendName:            "secondary",
		PartialOutputNeedle:             "partial ",
	}

	flagSet := flag.NewFlagSet("nightlycheck", flag.ContinueOnError)
	flagSet.SetOutput(os.Stderr)

	flagSet.StringVar(&cfg.Mode, "mode", cfg.Mode, "validation mode: benchmarks, stream-failures, or anthropic-adapter")
	flagSet.StringVar(&cfg.MockNonStreamPath, "mock-non-stream", "", "path to mock non-stream benchmark JSON")
	flagSet.StringVar(&cfg.MockStreamPath, "mock-stream", "", "path to mock stream benchmark JSON")
	flagSet.StringVar(&cfg.AdapterNonStreamPath, "adapter-non-stream", "", "path to adapter non-stream benchmark JSON")
	flagSet.StringVar(&cfg.AdapterStreamPath, "adapter-stream", "", "path to adapter stream benchmark JSON")
	flagSet.Float64Var(&cfg.MinSuccessRate, "min-success-rate", cfg.MinSuccessRate, "minimum success rate for benchmark summaries")
	flagSet.Int64Var(&cfg.MockNonStreamMaxLatencyP95MS, "mock-non-stream-max-latency-p95-ms", cfg.MockNonStreamMaxLatencyP95MS, "maximum allowed mock non-stream p95 latency in ms")
	flagSet.Int64Var(&cfg.MockStreamMaxLatencyP95MS, "mock-stream-max-latency-p95-ms", cfg.MockStreamMaxLatencyP95MS, "maximum allowed mock stream p95 latency in ms")
	flagSet.Int64Var(&cfg.MockStreamMaxTTFTP95MS, "mock-stream-max-ttft-p95-ms", cfg.MockStreamMaxTTFTP95MS, "maximum allowed mock stream p95 TTFT in ms")
	flagSet.Int64Var(&cfg.AdapterNonStreamMaxLatencyP95MS, "adapter-non-stream-max-latency-p95-ms", cfg.AdapterNonStreamMaxLatencyP95MS, "maximum allowed adapter non-stream p95 latency in ms")
	flagSet.Int64Var(&cfg.AdapterStreamMaxLatencyP95MS, "adapter-stream-max-latency-p95-ms", cfg.AdapterStreamMaxLatencyP95MS, "maximum allowed adapter stream p95 latency in ms")
	flagSet.Int64Var(&cfg.AdapterStreamMaxTTFTP95MS, "adapter-stream-max-ttft-p95-ms", cfg.AdapterStreamMaxTTFTP95MS, "maximum allowed adapter stream p95 TTFT in ms")
	flagSet.StringVar(&cfg.PrechunkOutputPath, "prechunk-output", "", "path to the pre-chunk stream drill output")
	flagSet.StringVar(&cfg.PrechunkMetricsPath, "prechunk-metrics", "", "path to the pre-chunk stream drill metrics")
	flagSet.StringVar(&cfg.PartialOutputPath, "partial-output", "", "path to the partial stream drill output")
	flagSet.StringVar(&cfg.PartialMetricsPath, "partial-metrics", "", "path to the partial stream drill metrics")
	flagSet.StringVar(&cfg.SystemOutputPath, "system-output", "", "path to the Anthropic system prompt drill output")
	flagSet.StringVar(&cfg.UpstreamRequestPath, "upstream-request", "", "path to the captured upstream request JSON")
	flagSet.StringVar(&cfg.PrimaryBackendName, "primary-backend", cfg.PrimaryBackendName, "primary backend name expected in metrics")
	flagSet.StringVar(&cfg.SecondaryBackendName, "secondary-backend", cfg.SecondaryBackendName, "secondary backend name expected in metrics")
	flagSet.StringVar(&cfg.PartialOutputNeedle, "partial-output-needle", cfg.PartialOutputNeedle, "output fragment expected in partial stream drill output")

	if err := flagSet.Parse(args); err != nil {
		return config{}, err
	}
	if cfg.MinSuccessRate < 0 || cfg.MinSuccessRate > 1 {
		return config{}, fmt.Errorf("-min-success-rate must be between 0 and 1")
	}

	return cfg, nil
}

func validateBenchmarkMode(cfg config) []string {
	expectations := []benchmarkExpectation{
		{
			Name:            "mock-non-stream",
			Path:            cfg.MockNonStreamPath,
			ExpectStream:    false,
			MinSuccessRate:  cfg.MinSuccessRate,
			MaxLatencyP95MS: cfg.MockNonStreamMaxLatencyP95MS,
		},
		{
			Name:            "mock-stream",
			Path:            cfg.MockStreamPath,
			ExpectStream:    true,
			MinSuccessRate:  cfg.MinSuccessRate,
			MaxLatencyP95MS: cfg.MockStreamMaxLatencyP95MS,
			MaxTTFTP95MS:    cfg.MockStreamMaxTTFTP95MS,
		},
		{
			Name:            "adapter-non-stream",
			Path:            cfg.AdapterNonStreamPath,
			ExpectStream:    false,
			MinSuccessRate:  cfg.MinSuccessRate,
			MaxLatencyP95MS: cfg.AdapterNonStreamMaxLatencyP95MS,
		},
		{
			Name:            "adapter-stream",
			Path:            cfg.AdapterStreamPath,
			ExpectStream:    true,
			MinSuccessRate:  cfg.MinSuccessRate,
			MaxLatencyP95MS: cfg.AdapterStreamMaxLatencyP95MS,
			MaxTTFTP95MS:    cfg.AdapterStreamMaxTTFTP95MS,
		},
	}

	var findings []string
	for _, expectation := range expectations {
		if expectation.Path == "" {
			findings = append(findings, fmt.Sprintf("%s artifact path is required", expectation.Name))
			continue
		}
		summary, err := loadBenchmarkSummary(expectation.Path)
		if err != nil {
			findings = append(findings, fmt.Sprintf(labelErrorFormat, expectation.Name, err))
			continue
		}
		findings = append(findings, validateBenchmarkSummary(expectation, summary)...)
	}
	return findings
}

func validateBenchmarkSummary(expectation benchmarkExpectation, summary benchmarkSummary) []string {
	var findings []string
	if summary.Requests <= 0 {
		findings = append(findings, fmt.Sprintf("%s has invalid requests count %d", expectation.Name, summary.Requests))
		return findings
	}
	if summary.Stream != expectation.ExpectStream {
		findings = append(findings, fmt.Sprintf("%s stream=%t, want %t", expectation.Name, summary.Stream, expectation.ExpectStream))
	}
	successRate := float64(summary.Success) / float64(summary.Requests)
	if successRate < expectation.MinSuccessRate {
		findings = append(findings, fmt.Sprintf("%s success rate %.3f below %.3f", expectation.Name, successRate, expectation.MinSuccessRate))
	}
	if summary.Failure > 0 {
		findings = append(findings, fmt.Sprintf("%s reported %d failed requests", expectation.Name, summary.Failure))
	}
	if summary.SampleError != "" {
		findings = append(findings, fmt.Sprintf("%s sample_error=%q", expectation.Name, summary.SampleError))
	}
	if status200 := summary.StatusCounts[200]; status200 != summary.Success {
		findings = append(findings, fmt.Sprintf("%s status_counts[200]=%d, want success=%d", expectation.Name, status200, summary.Success))
	}
	if expectation.MaxLatencyP95MS > 0 && summary.LatencyP95MS > expectation.MaxLatencyP95MS {
		findings = append(findings, fmt.Sprintf("%s latency_p95_ms=%d exceeded %d", expectation.Name, summary.LatencyP95MS, expectation.MaxLatencyP95MS))
	}
	if expectation.ExpectStream && summary.StreamChunks <= 0 {
		findings = append(findings, fmt.Sprintf("%s stream_chunks_total=%d, want > 0", expectation.Name, summary.StreamChunks))
	}
	if expectation.MaxTTFTP95MS > 0 && summary.TTFTP95MS > expectation.MaxTTFTP95MS {
		findings = append(findings, fmt.Sprintf("%s ttft_p95_ms=%d exceeded %d", expectation.Name, summary.TTFTP95MS, expectation.MaxTTFTP95MS))
	}
	return findings
}

type contentCheck struct {
	label  string
	body   string
	needle string
}

func validateStreamFailureMode(cfg config) []string {
	var findings []string
	primaryBackend := strings.TrimSpace(cfg.PrimaryBackendName)
	if primaryBackend == "" {
		primaryBackend = "openai-primary"
	}
	secondaryBackend := strings.TrimSpace(cfg.SecondaryBackendName)
	if secondaryBackend == "" {
		secondaryBackend = "secondary"
	}
	partialNeedle := cfg.PartialOutputNeedle
	if partialNeedle == "" {
		partialNeedle = "partial "
	}

	prechunkOutput, err := readFile(cfg.PrechunkOutputPath)
	if err != nil {
		findings = append(findings, fmt.Sprintf(labelErrorFormat, prechunkOutputLabel, err))
	} else {
		findings = append(findings, requireContains(contentCheck{label: prechunkOutputLabel, body: prechunkOutput, needle: httpStatusOKLine})...)
		findings = append(findings, requireContains(contentCheck{label: prechunkOutputLabel, body: prechunkOutput, needle: streamDoneMarker})...)
		findings = append(findings, requireContains(contentCheck{label: prechunkOutputLabel, body: prechunkOutput, needle: "chatcmpl-mock"})...)
	}

	partialOutput, err := readFile(cfg.PartialOutputPath)
	if err != nil {
		findings = append(findings, fmt.Sprintf(labelErrorFormat, partialOutputLabel, err))
	} else {
		findings = append(findings, requireContains(contentCheck{label: partialOutputLabel, body: partialOutput, needle: httpStatusOKLine})...)
		findings = append(findings, requireContains(contentCheck{label: partialOutputLabel, body: partialOutput, needle: partialNeedle})...)
		findings = append(findings, requireAbsent(contentCheck{label: partialOutputLabel, body: partialOutput, needle: streamDoneMarker})...)
	}

	prechunkMetrics, err := loadMetrics(cfg.PrechunkMetricsPath)
	if err != nil {
		findings = append(findings, fmt.Sprintf("prechunk metrics: %v", err))
	} else {
		findings = append(findings, requireMetricAtLeast("prechunk metrics", prechunkMetrics, fmt.Sprintf(`lag_provider_events_total{type="provider_request_failed",operation="stream",backend="%s"}`, primaryBackend), 1)...)
		findings = append(findings, requireMetricAtLeast("prechunk metrics", prechunkMetrics, fmt.Sprintf(`lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="%s"}`, secondaryBackend), 1)...)
	}

	partialMetrics, err := loadMetrics(cfg.PartialMetricsPath)
	if err != nil {
		findings = append(findings, fmt.Sprintf("partial metrics: %v", err))
	} else {
		findings = append(findings, requireMetricAtLeast("partial metrics", partialMetrics, fmt.Sprintf(`lag_provider_events_total{type="provider_stream_interrupted",operation="stream",backend="%s"}`, primaryBackend), 1)...)
		findings = append(findings, requireMetricAbsent("partial metrics", partialMetrics, fmt.Sprintf(`lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="%s"}`, secondaryBackend))...)
	}

	return findings
}

type recordedUpstreamRequest struct {
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Payload struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		System    string `json:"system"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	} `json:"payload"`
}

func validateAnthropicAdapterMode(cfg config) []string {
	var findings []string

	systemOutput, err := readFile(cfg.SystemOutputPath)
	if err != nil {
		findings = append(findings, fmt.Sprintf("system output: %v", err))
	} else {
		findings = append(findings, requireContains(contentCheck{label: "system output", body: systemOutput, needle: httpStatusOKLine})...)
		findings = append(findings, requireContains(contentCheck{label: "system output", body: systemOutput, needle: "messages=1;first_role=user"})...)
	}

	request, err := loadRecordedUpstreamRequest(cfg.UpstreamRequestPath)
	if err != nil {
		findings = append(findings, fmt.Sprintf("upstream request: %v", err))
	} else {
		findings = append(findings, validateRecordedAnthropicRequest(request)...)
	}

	findings = append(findings, validateStreamFailureMode(cfg)...)
	return findings
}

func validateRecordedAnthropicRequest(request recordedUpstreamRequest) []string {
	var findings []string

	if request.Path != "/v1/messages" {
		findings = append(findings, fmt.Sprintf("upstream request path=%q, want /v1/messages", request.Path))
	}
	findings = append(findings, validateAnthropicRequestHeaders(request.Headers)...)
	findings = append(findings, validateAnthropicRequestPayload(request.Payload)...)
	return findings
}

func validateAnthropicRequestHeaders(headers map[string]string) []string {
	var findings []string
	if strings.TrimSpace(headers["anthropic-version"]) == "" {
		findings = append(findings, "upstream request missing anthropic-version header")
	}
	if strings.TrimSpace(headers["x-api-key"]) == "" {
		findings = append(findings, "upstream request missing x-api-key header")
	}
	return findings
}

func validateAnthropicRequestPayload(payload struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	System    string `json:"system"`
	Messages  []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}) []string {
	var findings []string

	if payload.System != "Be concise.\n\nUse JSON only." {
		findings = append(findings, fmt.Sprintf("upstream request system=%q, want %q", payload.System, "Be concise.\n\nUse JSON only."))
	}
	if payload.MaxTokens <= 0 {
		findings = append(findings, fmt.Sprintf("upstream request max_tokens=%d, want > 0", payload.MaxTokens))
	}
	findings = append(findings, validateAnthropicRequestMessages(payload.Messages)...)
	return findings
}

func validateAnthropicRequestMessages(messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}) []string {
	var findings []string
	if len(messages) != 1 {
		findings = append(findings, fmt.Sprintf("upstream request messages=%d, want 1", len(messages)))
	} else {
		if messages[0].Role != "user" {
			findings = append(findings, fmt.Sprintf("upstream request first role=%q, want user", messages[0].Role))
		}
		if messages[0].Content != "reply in five words" {
			findings = append(findings, fmt.Sprintf("upstream request first content=%q, want %q", messages[0].Content, "reply in five words"))
		}
	}
	for _, message := range messages {
		if strings.EqualFold(strings.TrimSpace(message.Role), "system") {
			findings = append(findings, "upstream request unexpectedly forwarded a system role inside messages")
			break
		}
	}
	return findings
}

func loadRecordedUpstreamRequest(path string) (recordedUpstreamRequest, error) {
	body, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return recordedUpstreamRequest{}, err
	}
	var request recordedUpstreamRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return recordedUpstreamRequest{}, err
	}
	return request, nil
}

func loadBenchmarkSummary(path string) (benchmarkSummary, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return benchmarkSummary{}, err
	}

	var summary benchmarkSummary
	if err := json.Unmarshal(body, &summary); err != nil {
		return benchmarkSummary{}, err
	}
	return summary, nil
}

func loadMetrics(path string) (map[string]float64, error) {
	body, err := readFile(path)
	if err != nil {
		return nil, err
	}

	metrics := make(map[string]float64)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			continue
		}
		key := strings.Join(fields[:len(fields)-1], " ")
		metrics[key] = value
	}
	return metrics, nil
}

func readFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func requireContains(check contentCheck) []string {
	if strings.Contains(check.body, check.needle) {
		return nil
	}
	return []string{fmt.Sprintf("%s missing %q", check.label, check.needle)}
}

func requireAbsent(check contentCheck) []string {
	if !strings.Contains(check.body, check.needle) {
		return nil
	}
	return []string{fmt.Sprintf("%s unexpectedly contained %q", check.label, check.needle)}
}

func requireMetricAtLeast(label string, metrics map[string]float64, key string, minValue float64) []string {
	value, ok := metrics[key]
	if !ok {
		return []string{fmt.Sprintf("%s missing metric %s", label, key)}
	}
	if value < minValue {
		return []string{fmt.Sprintf("%s metric %s=%g below %g", label, key, value, minValue)}
	}
	return nil
}

func requireMetricAbsent(label string, metrics map[string]float64, key string) []string {
	value, ok := metrics[key]
	if !ok || value == 0 {
		return nil
	}
	return []string{fmt.Sprintf("%s metric %s=%g, want absent or 0", label, key, value)}
}
