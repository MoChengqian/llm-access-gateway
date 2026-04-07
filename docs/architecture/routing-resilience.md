# Routing and Resilience

## Overview

The LLM Access Gateway routes chat requests through an ordered set of provider backends and combines three resilience layers:

1. backend-local retry inside provider adapters
2. router-level fallback across backends
3. passive health tracking plus active probing

The current implementation is intentionally simple and evidence-based: it is an ordered primary/secondary failover design, not a weighted load balancer. Backend order comes from process configuration, unhealthy backends are skipped during cooldown when healthy alternatives exist, and streaming fallback stops once the first chunk has been accepted from an upstream provider.

## Routing Strategy

### Backend order is deterministic

The gateway builds exactly two chat backends, `primary` and `secondary`, in that order. `buildProviderBackends()` returns the slice `[primary, secondary]`, and the router preserves that order when choosing candidates.

```go
func buildProviderBackends(cfg config.Config) ([]providerrouter.Backend, []modelsservice.Source, error) {
    primary, err := buildProviderBackend("primary", cfg.Provider.Primary, cfg.Gateway.DefaultModel, providermock.Config{
        FailCreate: cfg.Gateway.PrimaryMockFailCreate,
        FailStream: cfg.Gateway.PrimaryMockFailStream,
    })
    secondary, err := buildProviderBackend("secondary", cfg.Provider.Secondary, cfg.Gateway.DefaultModel, providermock.Config{})
    return []providerrouter.Backend{primary, secondary}, sources, nil
}
```

This means:

- the primary backend is always attempted first when it is considered healthy
- the secondary backend is a failover target, not a load-balancing peer
- there is no weight field, percentage split, or random selection in the current code path

That last point matters because earlier handoff notes mention weight-based routing, but the shipped implementation does not currently support it.

### Candidate selection is health-aware

Before each request, the router calls `candidates()` to split configured backends into:

- `candidates`: backends that are not currently in cooldown
- `skipped`: backends whose `unhealthyUntil` timestamp is still in the future

If at least one healthy backend exists, the router only tries `candidates` and emits `provider_skipped_unhealthy` events for the skipped set.

If every backend is still in cooldown, `candidates()` returns all backends anyway:

```go
if len(candidates) == 0 {
    return append(candidates, skipped...), nil
}
```

So `/readyz` can report the gateway as not ready while request paths still make a best-effort attempt against all configured providers. Readiness is therefore an operational signal for load balancers, not a hard admission check inside the chat path.

## Retry and Fallback Layers

### Adapter-level retry happens before router fallback

Retry is implemented inside the OpenAI-compatible provider adapter, not inside `internal/provider/router/chat.go`. `doRequest()` retries the same backend up to `MaxRetries` times with linear backoff before returning control to the router.

Retryable conditions are:

- `408 Request Timeout`
- `429 Too Many Requests`
- `5xx` upstream responses
- network errors that satisfy `net.Error`
- `context.DeadlineExceeded`

```go
for attempt := 0; attempt <= p.maxRetries; attempt++ {
    resp, err := p.doRequestOnce(ctx, method, url, body, accept)
    if err == nil {
        return resp, nil
    }
    if attempt == p.maxRetries || !shouldRetryRequest(ctx, err) {
        return nil, err
    }
    if err := p.waitRetry(ctx, attempt); err != nil {
        return nil, lastErr
    }
}
```

This produces a two-stage flow for OpenAI backends:

1. retry the same upstream endpoint locally
2. if the backend still fails, let the router fall back to the next backend

Mock backends do not implement extra retry logic, so they fail directly into router fallback.

### Non-streaming fallback is sequential

For non-streaming requests, `CreateChatCompletion()` iterates through candidate backends in order:

1. start a backend attempt span
2. call `backend.Provider.CreateChatCompletion()`
3. on success, reset failure state with `markSuccess()`
4. on failure, record `provider_request_failed`, update health state, and continue

If a later backend succeeds, the router emits `provider_fallback_succeeded`.

The behavior is verified by `TestCreateCompletionFallsBackToSecondary` and `TestObserverSeesFallbackAndFailureEvents` in [`internal/provider/router/chat_test.go`](../../internal/provider/router/chat_test.go).

### Streaming fallback only exists before the first chunk

Streaming uses the same ordered backend iteration, but the router adds a strict first-chunk gate:

```go
events, err := backend.Provider.StreamChatCompletion(attemptCtx, req)
if err != nil {
    // try next backend
}

firstEvent, err := p.awaitFirstStreamEvent(attemptCtx, events)
if err != nil {
    // try next backend
}

return p.wrapStream(ctx, events, firstEvent, span, attemptSpan, backend.Name, index+1), nil
```

Fallback is allowed when:

- the stream fails to open
- the upstream stream closes before emitting a first chunk
- the first stream event is an error

Fallback is not allowed after the first successful chunk has been accepted. After that point, `forwardStreamEvent()` forwards the interruption to the caller, marks the backend unhealthy, emits `provider_stream_interrupted`, and closes the wrapped stream.

This boundary is covered by:

- `TestStreamCompletionFallsBackBeforeFirstChunk`
- `TestStreamCompletionFallsBackWhenPrimaryErrorsBeforeFirstChunk`
- `TestStreamCompletionDoesNotFallbackAfterFirstChunk`

## Passive Health Tracking and Cooldown

### Failure accounting

The router keeps in-memory state per backend:

- `consecutiveFailures`
- `unhealthyUntil`
- `lastProbeAt`
- `lastProbeError`

Each request failure calls `markFailure()`:

```go
state.consecutiveFailures++
if state.consecutiveFailures >= p.failureThreshold {
    state.unhealthyUntil = p.now().Add(p.cooldown)
}
```

Each successful request calls `markSuccess()`, which clears failure counters and cooldown state for that backend. `failureThreshold` and `cooldown` default to the values that ship with the gateway config:

- `gateway.provider_failure_threshold` defaults to `1`
- `gateway.provider_cooldown_seconds` defaults to `30`
- `gateway.provider_probe_interval_seconds` defaults to `30`

These defaults live in [`internal/config/config.go`](../../internal/config/config.go) and are enforced again inside `providerrouter.New()` so that even a nil or zero configuration translates to a 1-failure threshold and a 30-second cooldown.

### Probe results update the same health model

`Probe()` uses `ListModels()` against backends that also implement `provider.ModelProvider`. A failed probe increments the same failure counter and stores probe metadata with `markProbeFailure()`. A successful probe clears unhealthy state with `markProbeSuccess()` and records the probe timestamp.

Because probe results and request-path failures share the same state map, health can recover either because:

- a real request succeeds
- a background probe succeeds

The background probe loop is started in [`cmd/gateway/main.go`](../../cmd/gateway/main.go):

```go
if cfg.Gateway.ProviderProbeIntervalSeconds > 0 {
    startProviderProbeLoop(probeCtx, logger, chatProvider, time.Duration(cfg.Gateway.ProviderProbeIntervalSeconds)*time.Second)
}
```

`startProviderProbeLoop()` runs `Provider.Probe()` immediately and then on a ticker, so even when request traffic is low the router still gathers health signals.

## Readiness, Debug Endpoints, and Failure Visibility

### `/readyz`

The router is considered ready when at least one backend is currently healthy:

```go
func (p *Provider) Ready() bool {
    statuses := p.BackendStatuses()
    for _, status := range statuses {
        if status.Healthy {
            return true
        }
    }
    return false
}
```

The HTTP handler maps that state to:

- `200 OK` with `{"status":"ready"}` when any backend is healthy
- `503 Service Unavailable` with `{"status":"not ready"}` when all backends are unhealthy

### `/debug/providers`

`/debug/providers` exposes aggregate readiness plus per-backend status:

```json
{
  "ready": false,
  "providers": [
    {
      "name": "primary",
      "healthy": false,
      "consecutive_failures": 1,
      "unhealthy_until": "2026-04-07T10:00:30Z",
      "last_probe_at": "2026-04-07T10:00:00Z",
      "last_probe_error": "upstream status 502: temporary failure"
    }
  ]
}
```

This endpoint is the most direct way to understand why `/readyz` changed and which backend is cooling down.

### Reproducible inspection commands

```bash
curl -i http://127.0.0.1:8080/readyz
curl -i http://127.0.0.1:8080/debug/providers
curl -s http://127.0.0.1:8080/metrics | grep '^lag_provider'
```

For local failure drills, the repository already includes:

```bash
./scripts/provider-fallback-drill.sh create-fail
./scripts/provider-fallback-drill.sh stream-fail
```

## Resilience Signals in Logs and Metrics

Provider routing emits observer events that fan out to both logs and metrics:

- `provider_request_failed`
- `provider_request_succeeded`
- `provider_fallback_succeeded`
- `provider_skipped_unhealthy`
- `provider_stream_interrupted`
- `provider_probe_succeeded`
- `provider_probe_failed`
- `provider_recovered`

`cmd/gateway/main.go` wires a `multiProviderObserver` that sends the same event stream to:

- `providerEventLogger`, which writes structured zap logs
- `metrics.Registry`, which increments Prometheus-style counters

That shared observer path keeps failure diagnosis aligned across `/debug/providers`, logs, and `/metrics`.

## Design Boundaries

- Routing is ordered failover, not weighted balancing.
- Retry is provider-specific and happens before router fallback.
- Health tracking is in-memory and process-local; a restart clears counters and cooldown state.
- Streaming fallback ends at the first successful chunk boundary.
- `/readyz` reflects aggregate backend health but does not stop the router from making best-effort attempts when all backends are unhealthy.

## Related Documentation

- [Request Flow](request-flow.md)
- [Streaming Proxy Architecture](streaming-proxy.md)
- [Provider Adapter Design](provider-adapters.md)
- [Observability Design](observability.md)
- [Local Development](../local-development.md)

## Code References

- [`cmd/gateway/main.go`](../../cmd/gateway/main.go)
- [`internal/config/config.go`](../../internal/config/config.go)
- [`internal/provider/router/chat.go`](../../internal/provider/router/chat.go)
- [`internal/provider/router/chat_test.go`](../../internal/provider/router/chat_test.go)
- [`internal/provider/openai/chat.go`](../../internal/provider/openai/chat.go)
- [`internal/api/handlers/health.go`](../../internal/api/handlers/health.go)
