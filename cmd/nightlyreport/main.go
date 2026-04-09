package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type config struct {
	BaselinePath string

	MockNonStreamPath    string
	MockStreamPath       string
	AdapterNonStreamPath string
	AdapterStreamPath    string

	PrechunkOutputPath  string
	PrechunkMetricsPath string
	PartialOutputPath   string
	PartialMetricsPath  string

	AnthropicSystemOutputPath    string
	AnthropicUpstreamRequestPath string
	AnthropicPrechunkOutputPath  string
	AnthropicPrechunkMetricsPath string
	AnthropicPartialOutputPath   string
	AnthropicPartialMetricsPath  string

	OutputPath string
}

type baselineFile struct {
	Source     string                      `json:"source"`
	Benchmarks map[string]benchmarkSummary `json:"benchmarks"`
}

type benchmarkSummary struct {
	Requests        int         `json:"requests"`
	Concurrency     int         `json:"concurrency"`
	Stream          bool        `json:"stream"`
	TotalDurationMS int64       `json:"total_duration_ms"`
	Success         int         `json:"success"`
	Failure         int         `json:"failure"`
	StatusCounts    map[int]int `json:"status_counts"`
	LatencyP50MS    int64       `json:"latency_p50_ms"`
	LatencyP95MS    int64       `json:"latency_p95_ms"`
	LatencyMaxMS    int64       `json:"latency_max_ms"`
	TTFTP50MS       int64       `json:"ttft_p50_ms,omitempty"`
	TTFTP95MS       int64       `json:"ttft_p95_ms,omitempty"`
	TTFTMaxMS       int64       `json:"ttft_max_ms,omitempty"`
	StreamChunks    int         `json:"stream_chunks_total,omitempty"`
	SampleError     string      `json:"sample_error,omitempty"`
}

type streamScenarioSummary struct {
	Name    string
	Status  string
	Signals []string
}

type reportRow struct {
	Name          string
	Status        string
	Success       string
	LatencyP95    string
	TTFTP95       string
	TotalDuration string
	StreamChunks  string
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse flags: %v\n", err)
		os.Exit(1)
	}

	report, err := buildReport(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build report: %v\n", err)
		os.Exit(1)
	}

	if cfg.OutputPath == "" {
		fmt.Print(report)
		return
	}
	if err := os.WriteFile(cfg.OutputPath, []byte(report), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write report: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags(args []string) (config, error) {
	cfg := config{
		BaselinePath: ".github/nightly/benchmark-baseline.json",
	}

	flagSet := flag.NewFlagSet("nightlyreport", flag.ContinueOnError)
	flagSet.SetOutput(os.Stderr)
	flagSet.StringVar(&cfg.BaselinePath, "baseline", cfg.BaselinePath, "path to persisted benchmark baseline JSON")
	flagSet.StringVar(&cfg.MockNonStreamPath, "mock-non-stream", "", "path to mock non-stream benchmark JSON")
	flagSet.StringVar(&cfg.MockStreamPath, "mock-stream", "", "path to mock stream benchmark JSON")
	flagSet.StringVar(&cfg.AdapterNonStreamPath, "adapter-non-stream", "", "path to adapter non-stream benchmark JSON")
	flagSet.StringVar(&cfg.AdapterStreamPath, "adapter-stream", "", "path to adapter stream benchmark JSON")
	flagSet.StringVar(&cfg.PrechunkOutputPath, "prechunk-output", "", "path to the pre-chunk stream drill output")
	flagSet.StringVar(&cfg.PrechunkMetricsPath, "prechunk-metrics", "", "path to the pre-chunk stream drill metrics")
	flagSet.StringVar(&cfg.PartialOutputPath, "partial-output", "", "path to the partial stream drill output")
	flagSet.StringVar(&cfg.PartialMetricsPath, "partial-metrics", "", "path to the partial stream drill metrics")
	flagSet.StringVar(&cfg.AnthropicSystemOutputPath, "anthropic-system-output", "", "path to the Anthropic system prompt drill output")
	flagSet.StringVar(&cfg.AnthropicUpstreamRequestPath, "anthropic-upstream-request", "", "path to the captured Anthropic upstream request JSON")
	flagSet.StringVar(&cfg.AnthropicPrechunkOutputPath, "anthropic-prechunk-output", "", "path to the Anthropic pre-chunk stream drill output")
	flagSet.StringVar(&cfg.AnthropicPrechunkMetricsPath, "anthropic-prechunk-metrics", "", "path to the Anthropic pre-chunk stream drill metrics")
	flagSet.StringVar(&cfg.AnthropicPartialOutputPath, "anthropic-partial-output", "", "path to the Anthropic partial stream drill output")
	flagSet.StringVar(&cfg.AnthropicPartialMetricsPath, "anthropic-partial-metrics", "", "path to the Anthropic partial stream drill metrics")
	flagSet.StringVar(&cfg.OutputPath, "output", "", "path to write markdown report; stdout when omitted")
	if err := flagSet.Parse(args); err != nil {
		return config{}, err
	}
	return cfg, nil
}

func buildReport(cfg config) (string, error) {
	baseline, err := loadBaseline(cfg.BaselinePath)
	if err != nil {
		return "", err
	}

	currentSummaries := map[string]benchmarkSummary{}
	paths := map[string]string{
		"mock-non-stream":    cfg.MockNonStreamPath,
		"mock-stream":        cfg.MockStreamPath,
		"adapter-non-stream": cfg.AdapterNonStreamPath,
		"adapter-stream":     cfg.AdapterStreamPath,
	}
	for name, path := range paths {
		summary, err := loadBenchmarkSummary(path)
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		currentSummaries[name] = summary
	}

	streamScenarios, err := buildStreamScenarioSummaries(cfg)
	if err != nil {
		return "", err
	}
	anthropicScenarios, err := buildAnthropicScenarioSummaries(cfg)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString("# Nightly Verification Summary\n\n")
	builder.WriteString(fmt.Sprintf("Generated: `%s`\n\n", time.Now().UTC().Format(time.RFC3339)))
	builder.WriteString("## Benchmark Delta Vs Persisted Baseline\n\n")
	builder.WriteString(fmt.Sprintf("Baseline source: `%s`\n\n", baseline.Source))
	builder.WriteString("| Scenario | Status | Success | Latency P95 | TTFT P95 | Total Duration | Stream Chunks |\n")
	builder.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")

	rows := benchmarkRows(baseline.Benchmarks, currentSummaries)
	for _, row := range rows {
		builder.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s |\n",
			row.Name, row.Status, row.Success, row.LatencyP95, row.TTFTP95, row.TotalDuration, row.StreamChunks))
	}

	builder.WriteString("\n## Streaming Failure Drill Summary\n\n")
	builder.WriteString("| Scenario | Status | Signals |\n")
	builder.WriteString("| --- | --- | --- |\n")
	for _, scenario := range streamScenarios {
		builder.WriteString(fmt.Sprintf("| %s | %s | %s |\n", scenario.Name, scenario.Status, strings.Join(scenario.Signals, "<br>")))
	}

	builder.WriteString("\n## Anthropic Adapter Drill Summary\n\n")
	builder.WriteString("| Scenario | Status | Signals |\n")
	builder.WriteString("| --- | --- | --- |\n")
	for _, scenario := range anthropicScenarios {
		builder.WriteString(fmt.Sprintf("| %s | %s | %s |\n", scenario.Name, scenario.Status, strings.Join(scenario.Signals, "<br>")))
	}

	return builder.String(), nil
}

func benchmarkRows(baseline map[string]benchmarkSummary, current map[string]benchmarkSummary) []reportRow {
	names := make([]string, 0, len(current))
	for name := range current {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]reportRow, 0, len(names))
	for _, name := range names {
		base := baseline[name]
		now := current[name]
		rows = append(rows, reportRow{
			Name:          name,
			Status:        benchmarkStatus(base, now),
			Success:       fmt.Sprintf("%d/%d", now.Success, now.Requests),
			LatencyP95:    formatDelta(base.LatencyP95MS, now.LatencyP95MS, "ms"),
			TTFTP95:       formatOptionalDelta(base.TTFTP95MS, now.TTFTP95MS, "ms"),
			TotalDuration: formatDelta(base.TotalDurationMS, now.TotalDurationMS, "ms"),
			StreamChunks:  formatOptionalDelta(int64(base.StreamChunks), int64(now.StreamChunks), ""),
		})
	}
	return rows
}

func benchmarkStatus(base benchmarkSummary, now benchmarkSummary) string {
	if base.Requests == 0 {
		return "no-baseline"
	}
	switch {
	case now.Failure > 0 || now.Success < now.Requests:
		return "degraded"
	case now.LatencyP95MS > base.LatencyP95MS:
		return "slower"
	case now.LatencyP95MS < base.LatencyP95MS:
		return "faster"
	default:
		return "steady"
	}
}

func buildStreamScenarioSummaries(cfg config) ([]streamScenarioSummary, error) {
	prechunkOutput, err := readFile(cfg.PrechunkOutputPath)
	if err != nil {
		return nil, fmt.Errorf("pre-chunk output: %w", err)
	}
	prechunkMetrics, err := loadMetrics(cfg.PrechunkMetricsPath)
	if err != nil {
		return nil, fmt.Errorf("pre-chunk metrics: %w", err)
	}

	partialOutput, err := readFile(cfg.PartialOutputPath)
	if err != nil {
		return nil, fmt.Errorf("partial output: %w", err)
	}
	partialMetrics, err := loadMetrics(cfg.PartialMetricsPath)
	if err != nil {
		return nil, fmt.Errorf("partial metrics: %w", err)
	}

	return []streamScenarioSummary{
		{
			Name: "Pre-first-chunk fallback",
			Status: boolStatus(
				strings.Contains(prechunkOutput, "HTTP/1.1 200 OK") &&
					strings.Contains(prechunkOutput, "chatcmpl-mock") &&
					strings.Contains(prechunkOutput, "data: [DONE]") &&
					metricAtLeast(prechunkMetrics, `lag_provider_events_total{type="provider_request_failed",operation="stream",backend="openai-primary"}`, 1) &&
					metricAtLeast(prechunkMetrics, `lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="secondary"}`, 1),
			),
			Signals: []string{"200 OK observed", "fallback stream body observed", "[DONE] observed", "provider_request_failed recorded", "provider_fallback_succeeded recorded"},
		},
		{
			Name:    "Post-first-chunk interruption",
			Status:  boolStatus(strings.Contains(partialOutput, "HTTP/1.1 200 OK") && strings.Contains(partialOutput, "partial ") && !strings.Contains(partialOutput, "data: [DONE]") && metricAtLeast(partialMetrics, `lag_provider_events_total{type="provider_stream_interrupted",operation="stream",backend="openai-primary"}`, 1) && !metricPresent(partialMetrics, `lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="secondary"}`)),
			Signals: []string{"200 OK observed", "partial chunk observed", "no [DONE] marker", "interrupt metric recorded", "no fallback-after-first-chunk metric"},
		},
	}, nil
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

func buildAnthropicScenarioSummaries(cfg config) ([]streamScenarioSummary, error) {
	systemOutput, err := readFile(cfg.AnthropicSystemOutputPath)
	if err != nil {
		return nil, fmt.Errorf("anthropic system output: %w", err)
	}
	upstreamRequest, err := loadRecordedUpstreamRequest(cfg.AnthropicUpstreamRequestPath)
	if err != nil {
		return nil, fmt.Errorf("anthropic upstream request: %w", err)
	}
	prechunkOutput, err := readFile(cfg.AnthropicPrechunkOutputPath)
	if err != nil {
		return nil, fmt.Errorf("anthropic pre-chunk output: %w", err)
	}
	prechunkMetrics, err := loadMetrics(cfg.AnthropicPrechunkMetricsPath)
	if err != nil {
		return nil, fmt.Errorf("anthropic pre-chunk metrics: %w", err)
	}
	partialOutput, err := readFile(cfg.AnthropicPartialOutputPath)
	if err != nil {
		return nil, fmt.Errorf("anthropic partial output: %w", err)
	}
	partialMetrics, err := loadMetrics(cfg.AnthropicPartialMetricsPath)
	if err != nil {
		return nil, fmt.Errorf("anthropic partial metrics: %w", err)
	}

	systemOK := strings.Contains(systemOutput, "HTTP/1.1 200 OK") &&
		strings.Contains(systemOutput, "messages=1;first_role=user") &&
		upstreamRequest.Path == "/v1/messages" &&
		strings.TrimSpace(upstreamRequest.Headers["anthropic-version"]) != "" &&
		strings.TrimSpace(upstreamRequest.Headers["x-api-key"]) != "" &&
		upstreamRequest.Payload.System == "Be concise.\n\nUse JSON only." &&
		len(upstreamRequest.Payload.Messages) == 1 &&
		upstreamRequest.Payload.Messages[0].Role == "user" &&
		upstreamRequest.Payload.Messages[0].Content == "reply in five words"

	prechunkOK := strings.Contains(prechunkOutput, "HTTP/1.1 200 OK") &&
		strings.Contains(prechunkOutput, "chatcmpl-mock") &&
		strings.Contains(prechunkOutput, "data: [DONE]") &&
		metricAtLeast(prechunkMetrics, `lag_provider_events_total{type="provider_request_failed",operation="stream",backend="anthropic-primary"}`, 1) &&
		metricAtLeast(prechunkMetrics, `lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="secondary"}`, 1)

	partialOK := strings.Contains(partialOutput, "HTTP/1.1 200 OK") &&
		strings.Contains(partialOutput, "anthropic partial ") &&
		!strings.Contains(partialOutput, "data: [DONE]") &&
		metricAtLeast(partialMetrics, `lag_provider_events_total{type="provider_stream_interrupted",operation="stream",backend="anthropic-primary"}`, 1) &&
		!metricPresent(partialMetrics, `lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="secondary"}`)

	return []streamScenarioSummary{
		{
			Name:   "System prompt translation",
			Status: boolStatus(systemOK),
			Signals: []string{
				"200 OK observed",
				"gateway response confirms system join",
				"upstream captured /v1/messages",
				"anthropic-version header observed",
				"user-only message list observed",
			},
		},
		{
			Name:   "Anthropic pre-first-chunk fallback",
			Status: boolStatus(prechunkOK),
			Signals: []string{
				"200 OK observed",
				"fallback stream body observed",
				"[DONE] observed",
				"provider_request_failed recorded",
				"provider_fallback_succeeded recorded",
			},
		},
		{
			Name:   "Anthropic post-first-chunk interruption",
			Status: boolStatus(partialOK),
			Signals: []string{
				"200 OK observed",
				"anthropic partial chunk observed",
				"no [DONE] marker",
				"interrupt metric recorded",
				"no fallback-after-first-chunk metric",
			},
		},
	}, nil
}

func boolStatus(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}

func loadBaseline(path string) (baselineFile, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return baselineFile{}, err
	}
	var baseline baselineFile
	if err := json.Unmarshal(body, &baseline); err != nil {
		return baselineFile{}, err
	}
	return baseline, nil
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

func loadRecordedUpstreamRequest(path string) (recordedUpstreamRequest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return recordedUpstreamRequest{}, err
	}
	var request recordedUpstreamRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return recordedUpstreamRequest{}, err
	}
	return request, nil
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

func metricAtLeast(metrics map[string]float64, key string, minValue float64) bool {
	value, ok := metrics[key]
	return ok && value >= minValue
}

func metricPresent(metrics map[string]float64, key string) bool {
	value, ok := metrics[key]
	return ok && value > 0
}

func formatDelta(base int64, current int64, unit string) string {
	delta := current - base
	sign := "+"
	if delta < 0 {
		sign = ""
	}
	if unit != "" {
		return fmt.Sprintf("%d%s (%s%d%s)", current, unit, sign, delta, unit)
	}
	return fmt.Sprintf("%d (%s%d)", current, sign, delta)
}

func formatOptionalDelta(base int64, current int64, unit string) string {
	if base == 0 && current == 0 {
		return "n/a"
	}
	return formatDelta(base, current, unit)
}
