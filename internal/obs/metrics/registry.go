package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/api/handlers"
	"github.com/MoChengqian/llm-access-gateway/internal/provider/router"
)

type Registry struct {
	mu                   sync.Mutex
	httpRequests         map[string]uint64
	httpDurations        map[string]durationSample
	providerEvents       map[string]uint64
	providerDurations    map[string]durationSample
	probeResults         map[string]uint64
	governanceRejections map[string]uint64
	readyzFailures       uint64
	streamRequests       uint64
	streamChunks         uint64
	streamTTFTCount      uint64
	streamTTFTMillisSum  uint64
	providerStatuses     map[string]providerStatus
}

type durationSample struct {
	count uint64
	sumMS uint64
}

type providerStatus struct {
	healthy             bool
	consecutiveFailures int
	unhealthyUntil      time.Time
}

const (
	metricsHelpPrefix    = "# HELP "
	metricsTypePrefix    = "# TYPE "
	metricsCounterSuffix = " counter\n"
)

func NewRegistry() *Registry {
	return &Registry{
		httpRequests:         make(map[string]uint64),
		httpDurations:        make(map[string]durationSample),
		providerEvents:       make(map[string]uint64),
		providerDurations:    make(map[string]durationSample),
		probeResults:         make(map[string]uint64),
		governanceRejections: make(map[string]uint64),
		providerStatuses:     make(map[string]providerStatus),
	}
}

func (r *Registry) SyncProviderStatuses(statuses []handlers.ProviderBackendStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, status := range statuses {
		r.providerStatuses[status.Name] = providerStatus{
			healthy:             status.Healthy,
			consecutiveFailures: status.ConsecutiveFailures,
			unhealthyUntil:      status.UnhealthyUntil,
		}
	}
}

func (r *Registry) RecordHTTPRequest(method string, path string, status int, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf(`method="%s",path="%s",status="%d"`, sanitize(method), sanitize(path), status)
	r.httpRequests[key]++
	sample := r.httpDurations[key]
	sample.count++
	sample.sumMS += uint64(duration.Milliseconds())
	r.httpDurations[key] = sample
}

func (r *Registry) RecordReadyzFailure() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.readyzFailures++
}

func (r *Registry) RecordGovernanceRejection(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.governanceRejections[sanitize(reason)]++
}

func (r *Registry) RecordStreamRequest(ttft time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.streamRequests++
	r.streamTTFTCount++
	r.streamTTFTMillisSum += uint64(ttft.Milliseconds())
}

func (r *Registry) RecordStreamChunk() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.streamChunks++
}

func (r *Registry) OnEvent(event router.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf(`type="%s",operation="%s",backend="%s"`, sanitize(event.Type), sanitize(event.Operation), sanitize(event.Backend))
	r.providerEvents[key]++
	r.updateProviderStatus(event)
	if event.Duration > 0 && recordsProviderDuration(event.Type) {
		result := "success"
		if strings.Contains(event.Type, "failed") || strings.Contains(event.Type, "interrupted") {
			result = "error"
		}
		durationKey := fmt.Sprintf(`operation="%s",backend="%s",result="%s"`, sanitize(event.Operation), sanitize(event.Backend), result)
		sample := r.providerDurations[durationKey]
		sample.count++
		sample.sumMS += uint64(event.Duration.Milliseconds())
		r.providerDurations[durationKey] = sample
	}
	if event.Operation == "probe" {
		result := "success"
		if event.Type == "provider_probe_failed" {
			result = "error"
		}
		if event.Type == "provider_probe_succeeded" || event.Type == "provider_probe_failed" {
			probeKey := fmt.Sprintf(`backend="%s",result="%s"`, sanitize(event.Backend), result)
			r.probeResults[probeKey]++
		}
	}
}

func (r *Registry) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	var builder strings.Builder
	r.writeMetrics(&builder, now)

	_, _ = w.Write([]byte(builder.String()))
}

func (r *Registry) writeMetrics(builder *strings.Builder, now time.Time) {
	writeCounter(builder, "lag_http_requests_total", "Total HTTP requests handled by the gateway.", r.httpRequests)
	writeDurationSamples(builder,
		"lag_http_request_duration_milliseconds_sum",
		"Aggregate HTTP request duration in milliseconds.",
		"lag_http_request_duration_milliseconds_count",
		"Count of HTTP requests with latency recorded.",
		r.httpDurations,
	)
	writeCounter(builder, "lag_provider_events_total", "Total provider routing events.", r.providerEvents)
	writeDurationSamples(builder,
		"lag_provider_operation_duration_milliseconds_sum",
		"Aggregate provider operation duration in milliseconds.",
		"lag_provider_operation_duration_milliseconds_count",
		"Count of provider operations with latency recorded.",
		r.providerDurations,
	)
	writeCounter(builder, "lag_provider_probe_results_total", "Total provider probe results by backend and result.", r.probeResults)
	r.writeProviderStatusMetrics(builder, now)
	writeGovernanceMetrics(builder, r.governanceRejections, r.readyzFailures)
	writeStreamMetrics(builder, r.streamRequests, r.streamChunks, r.streamTTFTMillisSum, r.streamTTFTCount)
}

func (r *Registry) writeProviderStatusMetrics(builder *strings.Builder, now time.Time) {
	healthyBackends := 0
	builder.WriteString("# HELP lag_provider_backend_healthy Current provider backend health status (1=healthy, 0=unhealthy).\n")
	builder.WriteString("# TYPE lag_provider_backend_healthy gauge\n")
	for _, key := range sortedKeys(r.providerStatuses) {
		status := r.providerStatuses[key]
		if isProviderHealthy(status, now) {
			healthyBackends++
		}
		writeGauge(builder, "lag_provider_backend_healthy", fmt.Sprintf(`backend="%s"`, sanitize(key)), boolToGauge(isProviderHealthy(status, now)))
	}

	builder.WriteString("# HELP lag_provider_backend_consecutive_failures Current consecutive failure count by backend.\n")
	builder.WriteString("# TYPE lag_provider_backend_consecutive_failures gauge\n")
	for _, key := range sortedKeys(r.providerStatuses) {
		writeGauge(builder, "lag_provider_backend_consecutive_failures", fmt.Sprintf(`backend="%s"`, sanitize(key)), uint64(r.providerStatuses[key].consecutiveFailures))
	}

	builder.WriteString("# HELP lag_provider_backend_cooldown_remaining_milliseconds Remaining cooldown time before a backend is re-eligible.\n")
	builder.WriteString("# TYPE lag_provider_backend_cooldown_remaining_milliseconds gauge\n")
	for _, key := range sortedKeys(r.providerStatuses) {
		remaining := int64(0)
		if r.providerStatuses[key].unhealthyUntil.After(now) {
			remaining = r.providerStatuses[key].unhealthyUntil.Sub(now).Milliseconds()
		}
		writeGauge(builder, "lag_provider_backend_cooldown_remaining_milliseconds", fmt.Sprintf(`backend="%s"`, sanitize(key)), uint64(remaining))
	}

	builder.WriteString("# HELP lag_provider_ready Current provider readiness derived from backend health.\n")
	builder.WriteString("# TYPE lag_provider_ready gauge\n")
	if len(r.providerStatuses) == 0 || healthyBackends > 0 {
		builder.WriteString("lag_provider_ready 1\n")
	} else {
		builder.WriteString("lag_provider_ready 0\n")
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sanitize(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}

func recordsProviderDuration(eventType string) bool {
	switch eventType {
	case "provider_request_succeeded", "provider_request_failed", "provider_stream_interrupted", "provider_probe_succeeded", "provider_probe_failed":
		return true
	default:
		return false
	}
}

func writeCounter(builder *strings.Builder, metric string, help string, values map[string]uint64) {
	builder.WriteString(metricsHelpPrefix + metric + " " + help + "\n")
	builder.WriteString(metricsTypePrefix + metric + metricsCounterSuffix)
	for _, key := range sortedKeys(values) {
		builder.WriteString(metric + "{")
		builder.WriteString(key)
		builder.WriteString("} ")
		builder.WriteString(fmt.Sprintf("%d\n", values[key]))
	}
}

func writeDurationSamples(builder *strings.Builder, sumMetric string, sumHelp string, countMetric string, countHelp string, values map[string]durationSample) {
	builder.WriteString(metricsHelpPrefix + sumMetric + " " + sumHelp + "\n")
	builder.WriteString(metricsTypePrefix + sumMetric + metricsCounterSuffix)
	for _, key := range sortedKeys(values) {
		builder.WriteString(sumMetric + "{")
		builder.WriteString(key)
		builder.WriteString("} ")
		builder.WriteString(fmt.Sprintf("%d\n", values[key].sumMS))
	}

	builder.WriteString(metricsHelpPrefix + countMetric + " " + countHelp + "\n")
	builder.WriteString(metricsTypePrefix + countMetric + metricsCounterSuffix)
	for _, key := range sortedKeys(values) {
		builder.WriteString(countMetric + "{")
		builder.WriteString(key)
		builder.WriteString("} ")
		builder.WriteString(fmt.Sprintf("%d\n", values[key].count))
	}
}

func writeGovernanceMetrics(builder *strings.Builder, rejections map[string]uint64, readyzFailures uint64) {
	builder.WriteString("# HELP lag_governance_rejections_total Total governance rejections by reason.\n")
	builder.WriteString("# TYPE lag_governance_rejections_total counter\n")
	for _, key := range sortedKeys(rejections) {
		builder.WriteString(`lag_governance_rejections_total{reason="`)
		builder.WriteString(key)
		builder.WriteString(`"} `)
		builder.WriteString(fmt.Sprintf("%d\n", rejections[key]))
	}

	builder.WriteString("# HELP lag_readyz_failures_total Total /readyz abnormal responses.\n")
	builder.WriteString("# TYPE lag_readyz_failures_total counter\n")
	builder.WriteString(fmt.Sprintf("lag_readyz_failures_total %d\n", readyzFailures))
}

func writeStreamMetrics(builder *strings.Builder, requests uint64, chunks uint64, ttftSum uint64, ttftCount uint64) {
	builder.WriteString("# HELP lag_stream_requests_total Total streamed responses that emitted at least one chunk.\n")
	builder.WriteString("# TYPE lag_stream_requests_total counter\n")
	builder.WriteString(fmt.Sprintf("lag_stream_requests_total %d\n", requests))

	builder.WriteString("# HELP lag_stream_chunks_total Total SSE data chunks emitted before [DONE].\n")
	builder.WriteString("# TYPE lag_stream_chunks_total counter\n")
	builder.WriteString(fmt.Sprintf("lag_stream_chunks_total %d\n", chunks))

	builder.WriteString("# HELP lag_stream_ttft_milliseconds_sum Aggregate TTFT in milliseconds for streamed responses.\n")
	builder.WriteString("# TYPE lag_stream_ttft_milliseconds_sum counter\n")
	builder.WriteString(fmt.Sprintf("lag_stream_ttft_milliseconds_sum %d\n", ttftSum))

	builder.WriteString("# HELP lag_stream_ttft_milliseconds_count Count of streamed responses with TTFT recorded.\n")
	builder.WriteString("# TYPE lag_stream_ttft_milliseconds_count counter\n")
	builder.WriteString(fmt.Sprintf("lag_stream_ttft_milliseconds_count %d\n", ttftCount))
}

func writeGauge(builder *strings.Builder, metric string, labels string, value uint64) {
	builder.WriteString(metric)
	if labels != "" {
		builder.WriteString("{")
		builder.WriteString(labels)
		builder.WriteString("}")
	}
	builder.WriteString(" ")
	builder.WriteString(fmt.Sprintf("%d\n", value))
}

func boolToGauge(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}

func (r *Registry) updateProviderStatus(event router.Event) {
	if event.Backend == "" {
		return
	}

	status := r.providerStatuses[event.Backend]
	switch event.Type {
	case "provider_request_succeeded", "provider_probe_succeeded", "provider_recovered":
		status.healthy = true
		status.consecutiveFailures = 0
		status.unhealthyUntil = time.Time{}
	case "provider_request_failed", "provider_stream_interrupted", "provider_probe_failed":
		status.consecutiveFailures = event.ConsecutiveFailures
		status.unhealthyUntil = event.UnhealthyUntil
		status.healthy = event.UnhealthyUntil.IsZero()
	default:
		return
	}

	r.providerStatuses[event.Backend] = status
}

func isProviderHealthy(status providerStatus, now time.Time) bool {
	if status.unhealthyUntil.After(now) {
		return false
	}
	if !status.unhealthyUntil.IsZero() && !status.unhealthyUntil.After(now) {
		return true
	}
	return status.healthy
}
