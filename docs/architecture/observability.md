# Observability Design

## Overview

The LLM Access Gateway uses three concrete observability mechanisms:

1. request and trace identifiers propagated through HTTP headers and `context.Context`
2. structured JSON logs produced by zap
3. a Prometheus-style plaintext `/metrics` endpoint backed by an in-process registry

The implementation is intentionally lightweight. There is no external tracing backend or metrics exporter setup in the current codebase. Instead, trace spans are logged, metrics are accumulated in memory, and correlation depends on `request_id`, `trace_id`, and `span_id` being present across layers.

## Request and Trace Context Propagation

### Router middleware establishes the correlation envelope

The HTTP router applies middleware in this order:

```go
r.Use(chimiddleware.RequestID)
r.Use(chimiddleware.RealIP)
r.Use(chimiddleware.Recoverer)
r.Use(requestIDHeader)
r.Use(requestTracing(logger))
r.Use(requestMetrics(metricsRecorder))
r.Use(requestLogger(logger))
```

That sequence creates the basic observability contract for every request:

- `chimiddleware.RequestID` generates a request ID
- `requestIDHeader` copies it to `X-Request-Id`
- `requestTracing` starts the root span and copies the trace ID to `X-Trace-Id`
- `requestMetrics` records request duration and status
- `requestLogger` logs the completed request with correlation fields

`internal/api/router_test.go` verifies that both headers are always present and that the default trace ID matches the request ID.

### Root spans derive trace IDs from request IDs

`tracing.StartRequestSpan()` uses the request ID as the root trace ID when one is available:

```go
traceID := requestID
if traceID == "" {
    traceID = nextID()
}
```

This keeps client-visible headers and internal span traces aligned by default:

- `X-Request-Id` comes from chi request middleware
- `X-Trace-Id` comes from the tracing package
- both values match for normal HTTP requests

### Child spans preserve request and trace context

Every deeper layer uses `tracing.StartSpan()` to create child spans that inherit:

- `trace_id`
- `request_id`
- parent `span_id`

Representative span boundaries in the request path include:

- `chat.handler.create_completion` / `chat.handler.stream_completion`
- `chat.service.create_completion` / `chat.service.stream_completion`
- `provider.router.create` / `provider.router.stream`
- `provider.backend.create` / `provider.backend.stream`

This means a single request can be followed end to end without an external tracer, as long as logs are retained.

## Structured Logging

### Gateway logger

`cmd/gateway/main.go` creates the process logger with `zap.NewProductionConfig()`, so logs are emitted in structured production format rather than plain text.

### Request completion logs

`requestLogger()` emits one `http request completed` entry after each handler returns. The log contains:

- `request_id`
- `trace_id`
- `span_id`
- `method`
- `path`
- `status`
- `bytes`
- `duration`
- `real_ip`
- `user_agent`
- `content_type`

When authentication succeeds, it also adds:

- `tenant_name`
- `tenant_id`
- `api_key_id`
- `api_key_prefix`

The raw API key is not logged.

### Span completion logs

Every tracing span ends with a `trace span finished` entry. `Span.End()` appends:

- `trace_id`
- `span_id`
- `parent_span_id`
- `request_id`
- `span_name`
- `status`
- `duration`

If the span ended with an error, the log also includes `error`.

Because spans are emitted from the handler, service, router, and provider layers, the log stream gives you a hierarchical trace without requiring OTLP, Jaeger, or Tempo.

### Provider event logs

The provider observer path emits `provider event` logs for routing and health transitions. Fields include:

- `type`
- `operation`
- `backend`
- `attempt`
- `consecutive_failures`
- `duration` when available
- `unhealthy_until` when cooldown is active
- `reason` when an error exists

This is the log stream that explains fallback, probe failures, skipped unhealthy backends, and backend recovery.

## Metrics Exposed on `/metrics`

### Registry design

`internal/obs/metrics/registry.go` implements a mutex-protected in-memory registry that serves Prometheus text format directly through `ServeHTTP()`.

Important operational characteristics:

- metrics are process-local
- metrics reset on process restart
- the implementation publishes counters and count/sum pairs, not histogram buckets

### HTTP request metrics

The router middleware records request totals and aggregate latency:

- `lag_http_requests_total{method,path,status}`
- `lag_http_request_duration_milliseconds_sum{method,path,status}`
- `lag_http_request_duration_milliseconds_count{method,path,status}`

These are recorded for all routes, including `/healthz`, `/readyz`, and `/metrics`.

### Provider routing metrics

Provider observer events are translated into metrics:

- `lag_provider_events_total{type,operation,backend}`
- `lag_provider_operation_duration_milliseconds_sum{operation,backend,result}`
- `lag_provider_operation_duration_milliseconds_count{operation,backend,result}`
- `lag_provider_probe_results_total{backend,result}`
- `lag_provider_backend_healthy{backend}`
- `lag_provider_backend_consecutive_failures{backend}`
- `lag_provider_backend_cooldown_remaining_milliseconds{backend}`
- `lag_provider_ready`

`result="error"` is used for failed or interrupted provider operations, while successful attempts are labeled `result="success"`.

The first four metrics are historical counters or count/sum pairs. The latter four are current-state gauges derived from the latest known backend status, so `/metrics` can answer both:

- what happened recently
- what the router believes right now

### Governance and readiness metrics

The chat handler and router middleware also publish:

- `lag_governance_rejections_total{reason}`
- `lag_readyz_failures_total`

`lag_readyz_failures_total` increments whenever `/readyz` returns a non-200 response.

### Streaming metrics

Streaming-specific metrics are recorded only after the first chunk has actually been written:

- `lag_stream_requests_total`
- `lag_stream_chunks_total`
- `lag_stream_ttft_milliseconds_sum`
- `lag_stream_ttft_milliseconds_count`

This is important because a stream that fails before the first chunk does not count as a successful streamed response.

### Reproducible metrics inspection

```bash
curl -s http://127.0.0.1:8080/metrics
curl -s http://127.0.0.1:8080/metrics | grep '^lag_http_requests_total'
curl -s http://127.0.0.1:8080/metrics | grep '^lag_provider'
curl -s http://127.0.0.1:8080/metrics | grep '^lag_stream'
curl -s http://127.0.0.1:8080/metrics | grep '^lag_provider_backend_healthy'
```

## What a Correlated Request Looks Like

### Response headers

Any HTTP route served by the router includes correlation headers:

```bash
curl -i http://127.0.0.1:8080/healthz
```

You should see:

```text
X-Request-Id: 3db1e0d5f62b4c32
X-Trace-Id: 3db1e0d5f62b4c32
```

### Representative log records

The following examples use the actual field names emitted by the code:

```json
{"msg":"trace span finished","trace_id":"3db1e0d5f62b4c32","span_id":"0000000000000004","parent_span_id":"0000000000000003","request_id":"3db1e0d5f62b4c32","span_name":"provider.backend.create","status":"ok","duration":"812ms","backend":"primary","attempt":1}
```

```json
{"msg":"http request completed","request_id":"3db1e0d5f62b4c32","trace_id":"3db1e0d5f62b4c32","span_id":"0000000000000001","method":"POST","path":"/v1/chat/completions","status":200,"bytes":512,"duration":"845ms","tenant_name":"local-dev","tenant_id":1,"api_key_id":1,"api_key_prefix":"lag-"}
```

```json
{"msg":"provider event","type":"provider_fallback_succeeded","operation":"create","backend":"secondary","attempt":2,"consecutive_failures":0,"duration":"205ms"}
```

Zap adds its own metadata such as level and timestamp around these fields.

## Operational Debugging Workflow

When a request misbehaves, the fastest debugging loop is:

1. capture `X-Request-Id` and `X-Trace-Id` from the response
2. search logs for `request_id` or `trace_id`
3. inspect `provider event` and `trace span finished` entries to find the failing layer
4. check `/debug/providers` if the issue looks like routing or cooldown
5. check `/metrics` for aggregate signs such as readiness failures, governance rejections, or stream TTFT changes

Because request logs, span logs, and provider events all share the same correlation fields, this workflow works even without an external tracing system.

## Current Boundaries

- Tracing is log-based, not exporter-based.
- Metrics are in-memory and reset on restart.
- The registry exposes count and sum pairs, not percentile histograms.
- `/metrics`, `/healthz`, `/readyz`, and `/debug/providers` are registered directly on the router and are not protected by API-key auth.
- Streaming latency is split into two views: full request duration from middleware, and TTFT from the chat handler.

## Related Documentation

- [Request Flow](request-flow.md)
- [Routing and Resilience](routing-resilience.md)
- [Streaming Proxy Architecture](streaming-proxy.md)
- [API Endpoints](../api/endpoints.md)
- [Local Development](../local-development.md)

## Code References

- [`cmd/gateway/main.go`](../../cmd/gateway/main.go)
- [`internal/api/router.go`](../../internal/api/router.go)
- [`internal/api/router_test.go`](../../internal/api/router_test.go)
- [`internal/api/handlers/chat.go`](../../internal/api/handlers/chat.go)
- [`internal/obs/tracing/tracing.go`](../../internal/obs/tracing/tracing.go)
- [`internal/obs/metrics/registry.go`](../../internal/obs/metrics/registry.go)
- [`internal/service/chat/service.go`](../../internal/service/chat/service.go)
- [`internal/provider/router/chat.go`](../../internal/provider/router/chat.go)
