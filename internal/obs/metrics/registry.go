package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/provider/router"
)

type Registry struct {
	mu                   sync.Mutex
	httpRequests         map[string]uint64
	providerEvents       map[string]uint64
	providerDurations    map[string]durationSample
	probeResults         map[string]uint64
	governanceRejections map[string]uint64
	readyzFailures       uint64
	streamRequests       uint64
	streamChunks         uint64
	streamTTFTCount      uint64
	streamTTFTMillisSum  uint64
}

type durationSample struct {
	count uint64
	sumMS uint64
}

func NewRegistry() *Registry {
	return &Registry{
		httpRequests:         make(map[string]uint64),
		providerEvents:       make(map[string]uint64),
		providerDurations:    make(map[string]durationSample),
		probeResults:         make(map[string]uint64),
		governanceRejections: make(map[string]uint64),
	}
}

func (r *Registry) RecordHTTPRequest(method string, path string, status int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := fmt.Sprintf(`method="%s",path="%s",status="%d"`, sanitize(method), sanitize(path), status)
	r.httpRequests[key]++
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

	var builder strings.Builder
	builder.WriteString("# HELP lag_http_requests_total Total HTTP requests handled by the gateway.\n")
	builder.WriteString("# TYPE lag_http_requests_total counter\n")
	for _, key := range sortedKeys(r.httpRequests) {
		builder.WriteString("lag_http_requests_total{")
		builder.WriteString(key)
		builder.WriteString("} ")
		builder.WriteString(fmt.Sprintf("%d\n", r.httpRequests[key]))
	}

	builder.WriteString("# HELP lag_provider_events_total Total provider routing events.\n")
	builder.WriteString("# TYPE lag_provider_events_total counter\n")
	for _, key := range sortedKeys(r.providerEvents) {
		builder.WriteString("lag_provider_events_total{")
		builder.WriteString(key)
		builder.WriteString("} ")
		builder.WriteString(fmt.Sprintf("%d\n", r.providerEvents[key]))
	}

	builder.WriteString("# HELP lag_provider_operation_duration_milliseconds_sum Aggregate provider operation duration in milliseconds.\n")
	builder.WriteString("# TYPE lag_provider_operation_duration_milliseconds_sum counter\n")
	for _, key := range sortedKeys(r.providerDurations) {
		builder.WriteString("lag_provider_operation_duration_milliseconds_sum{")
		builder.WriteString(key)
		builder.WriteString("} ")
		builder.WriteString(fmt.Sprintf("%d\n", r.providerDurations[key].sumMS))
	}

	builder.WriteString("# HELP lag_provider_operation_duration_milliseconds_count Count of provider operations with latency recorded.\n")
	builder.WriteString("# TYPE lag_provider_operation_duration_milliseconds_count counter\n")
	for _, key := range sortedKeys(r.providerDurations) {
		builder.WriteString("lag_provider_operation_duration_milliseconds_count{")
		builder.WriteString(key)
		builder.WriteString("} ")
		builder.WriteString(fmt.Sprintf("%d\n", r.providerDurations[key].count))
	}

	builder.WriteString("# HELP lag_provider_probe_results_total Total provider probe results by backend and result.\n")
	builder.WriteString("# TYPE lag_provider_probe_results_total counter\n")
	for _, key := range sortedKeys(r.probeResults) {
		builder.WriteString("lag_provider_probe_results_total{")
		builder.WriteString(key)
		builder.WriteString("} ")
		builder.WriteString(fmt.Sprintf("%d\n", r.probeResults[key]))
	}

	builder.WriteString("# HELP lag_governance_rejections_total Total governance rejections by reason.\n")
	builder.WriteString("# TYPE lag_governance_rejections_total counter\n")
	for _, key := range sortedKeys(r.governanceRejections) {
		builder.WriteString(`lag_governance_rejections_total{reason="`)
		builder.WriteString(key)
		builder.WriteString(`"} `)
		builder.WriteString(fmt.Sprintf("%d\n", r.governanceRejections[key]))
	}

	builder.WriteString("# HELP lag_readyz_failures_total Total /readyz abnormal responses.\n")
	builder.WriteString("# TYPE lag_readyz_failures_total counter\n")
	builder.WriteString(fmt.Sprintf("lag_readyz_failures_total %d\n", r.readyzFailures))

	builder.WriteString("# HELP lag_stream_requests_total Total streamed responses that emitted at least one chunk.\n")
	builder.WriteString("# TYPE lag_stream_requests_total counter\n")
	builder.WriteString(fmt.Sprintf("lag_stream_requests_total %d\n", r.streamRequests))

	builder.WriteString("# HELP lag_stream_chunks_total Total SSE data chunks emitted before [DONE].\n")
	builder.WriteString("# TYPE lag_stream_chunks_total counter\n")
	builder.WriteString(fmt.Sprintf("lag_stream_chunks_total %d\n", r.streamChunks))

	builder.WriteString("# HELP lag_stream_ttft_milliseconds_sum Aggregate TTFT in milliseconds for streamed responses.\n")
	builder.WriteString("# TYPE lag_stream_ttft_milliseconds_sum counter\n")
	builder.WriteString(fmt.Sprintf("lag_stream_ttft_milliseconds_sum %d\n", r.streamTTFTMillisSum))

	builder.WriteString("# HELP lag_stream_ttft_milliseconds_count Count of streamed responses with TTFT recorded.\n")
	builder.WriteString("# TYPE lag_stream_ttft_milliseconds_count counter\n")
	builder.WriteString(fmt.Sprintf("lag_stream_ttft_milliseconds_count %d\n", r.streamTTFTCount))

	_, _ = w.Write([]byte(builder.String()))
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
