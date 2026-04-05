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
	governanceRejections map[string]uint64
	readyzFailures       uint64
	streamRequests       uint64
	streamChunks         uint64
	streamTTFTCount      uint64
	streamTTFTMillisSum  uint64
}

func NewRegistry() *Registry {
	return &Registry{
		httpRequests:         make(map[string]uint64),
		providerEvents:       make(map[string]uint64),
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
