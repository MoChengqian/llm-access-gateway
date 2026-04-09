# Anthropic Adapter Drill

## Overview

This report captures three Anthropic-specific verification runs executed locally on April 9, 2026:

1. system prompt translation into Anthropic's top-level `system` field
2. streaming failure before the first chunk, where fallback is still allowed
3. streaming interruption after the first chunk, where fallback must not happen

The drill used:

- gateway base URL: `http://127.0.0.1:18080`
- synthetic Anthropic upstream: `http://127.0.0.1:18081`
- development API key: `lag-local-dev-key`
- primary backend: `anthropic-primary`
- secondary backend: default mock provider

## Scenario A: System Prompt Translation

### Synthetic Upstream

```bash
python3 ./scripts/synthetic-anthropic-upstream.py --port 18081 --mode capture_system
```

### Gateway Configuration

```bash
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'
export APP_SERVER_ADDRESS='127.0.0.1:18080'
export APP_GATEWAY_DEFAULT_MODEL='claude-3-5-sonnet-latest'
export APP_PROVIDER_PRIMARY_TYPE='anthropic'
export APP_PROVIDER_PRIMARY_NAME='anthropic-primary'
export APP_PROVIDER_PRIMARY_BASE_URL='http://127.0.0.1:18081/v1'
export APP_PROVIDER_PRIMARY_API_KEY='sk-ant-local'
export APP_PROVIDER_PRIMARY_MODEL='claude-3-5-sonnet-latest'
export APP_PROVIDER_PRIMARY_MAX_TOKENS='1024'
export APP_PROVIDER_PRIMARY_TIMEOUT_SECONDS='1'
export APP_PROVIDER_PRIMARY_MAX_RETRIES='1'
export APP_PROVIDER_PRIMARY_RETRY_BACKOFF_MILLISECONDS='200'

go run ./cmd/gateway
BASE_URL='http://127.0.0.1:18080' \
UPSTREAM_BASE_URL='http://127.0.0.1:18081' \
MODEL='claude-3-5-sonnet-latest' \
./scripts/anthropic-adapter-drill.sh system-prompt
```

### Observed Client Output

The gateway returned a normal OpenAI-compatible JSON response, but the synthetic upstream reflected the translated request shape back through assistant content:

```text
HTTP/1.1 200 OK
Content-Type: application/json
...
{"id":"msg-upstream-1","object":"chat.completion","created":1775703215,"model":"claude-3-5-sonnet-latest","choices":[{"index":0,"message":{"role":"assistant","content":"system=Be concise.\n\nUse JSON only.;messages=1;first_role=user"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}
```

### Captured Upstream Request

The synthetic upstream's `/debug/last-request` endpoint showed that the adapter:

- joined both `system` messages into one top-level `system` string
- preserved only the `user` message in `messages[]`
- sent the Anthropic-specific headers

```json
{"path":"/v1/messages","headers":{"x-api-key":"sk-ant-local","anthropic-version":"2023-06-01","accept":"","content-type":"application/json"},"payload":{"model":"claude-3-5-sonnet-latest","max_tokens":1024,"system":"Be concise.\n\nUse JSON only.","messages":[{"role":"user","content":"reply in five words"}]}}
```

## Scenario B: Failure Before The First Chunk

### Synthetic Upstream

```bash
python3 ./scripts/synthetic-anthropic-upstream.py --port 18081 --mode stream_prechunk_fail
```

### Drill Command

```bash
BASE_URL='http://127.0.0.1:18080' ./scripts/provider-fallback-drill.sh stream-fail
```

### Observed Client Output

The client still received a complete fallback SSE response from the secondary mock backend, including the final `data: [DONE]` marker:

```text
HTTP/1.1 200 OK
Content-Type: text/event-stream
...
data: {"id":"chatcmpl-mock",...}
...
data: [DONE]
```

### Provider State During The Drill

Immediately after the failed Anthropic attempt, `/debug/providers` showed the primary backend in cooldown while readiness stayed green because the secondary backend remained healthy:

```json
{"providers":[{"name":"anthropic-primary","priority":100,"healthy":false,"consecutive_failures":1,"unhealthy_until":"2026-04-09T10:54:50.608093+08:00","last_probe_at":"2026-04-09T10:54:09.339991+08:00"},{"name":"secondary","priority":200,"healthy":true,"consecutive_failures":0,"unhealthy_until":"0001-01-01T00:00:00Z","last_probe_at":"2026-04-09T10:54:09.340871+08:00"}],"ready":true}
```

The background probe loop later recovered `anthropic-primary` at `10:54:39 +08:00`, which is why later snapshots returned to healthy status.

### Observability Output

Gateway log summary:

```text
provider_request_failed operation=stream backend=anthropic-primary duration=205.148125ms unhealthy_until=2026-04-09T10:54:50+08:00 reason=upstream status 500: synthetic anthropic prechunk failure
provider_request_succeeded operation=stream backend=secondary attempt=2 duration=90.125µs
provider_fallback_succeeded operation=stream backend=secondary attempt=2 duration=90.125µs
```

Metrics snapshot:

```text
lag_provider_events_total{type="provider_request_failed",operation="stream",backend="anthropic-primary"} 1
lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="secondary"} 1
lag_stream_requests_total 1
lag_stream_chunks_total 4
lag_stream_ttft_milliseconds_count 1
```

The synthetic upstream logged two `500` responses, confirming that the Anthropic adapter retried once before the router fell back to the secondary backend.

## Scenario C: Failure After The First Chunk

### Synthetic Upstream

```bash
python3 ./scripts/synthetic-anthropic-upstream.py --port 18081 --mode stream_partial
```

### Drill Command

```bash
BASE_URL='http://127.0.0.1:18080' \
UPSTREAM_BASE_URL='http://127.0.0.1:18081' \
MODEL='claude-3-5-sonnet-latest' \
./scripts/anthropic-adapter-drill.sh partial-stream
```

### Observed Client Output

The gateway emitted the first upstream chunk and then ended the stream without emitting a false fallback stream or a false `[DONE]` marker:

```text
HTTP/1.1 200 OK
Content-Type: text/event-stream
...
data: {"id":"msg-upstream-1","object":"chat.completion.chunk","created":1775703352,"model":"claude-3-5-sonnet-latest","choices":[{"index":0,"delta":{"role":"assistant","content":"anthropic partial "},"finish_reason":""}]}
```

### Observability Output

Gateway log summary:

```text
provider_request_succeeded operation=stream backend=anthropic-primary attempt=1 duration=25.867375ms
provider_stream_interrupted operation=stream backend=anthropic-primary attempt=1 duration=25.076875ms unhealthy_until=2026-04-09T10:56:22+08:00 reason=upstream stream ended before message_stop
```

Metrics snapshot:

```text
lag_provider_events_total{type="provider_request_succeeded",operation="stream",backend="anthropic-primary"} 1
lag_provider_events_total{type="provider_stream_interrupted",operation="stream",backend="anthropic-primary"} 1
lag_stream_requests_total 1
lag_stream_chunks_total 1
lag_stream_ttft_milliseconds_count 1
```

Notably, there was no `provider_fallback_succeeded` metric in this scenario.

## Analysis

These three runs demonstrate the Anthropic adapter's current contract with the gateway:

- request normalization is explicit, not implicit: system prompts are rewritten inside the adapter before the request leaves the process
- retry stays adapter-local: the adapter retries retryable upstream failures before the router switches backends
- streaming fallback still obeys the same architectural boundary as every other provider: only before the first chunk

That keeps the gateway behavior consistent even though Anthropic uses a different request schema and a named-event SSE protocol.

## Related Documentation

- [Provider Adapter Design](../../architecture/provider-adapters.md)
- [Routing and Resilience](../../architecture/routing-resilience.md)
- [Streaming Failure Drill](streaming-failures.md)
- [Configuration Reference](../../deployment/configuration.md)

## Code References

- [`internal/provider/anthropic/chat.go`](../../../internal/provider/anthropic/chat.go)
- [`cmd/gateway/main.go`](../../../cmd/gateway/main.go)
- [`scripts/synthetic-anthropic-upstream.py`](../../../scripts/synthetic-anthropic-upstream.py)
- [`scripts/anthropic-adapter-drill.sh`](../../../scripts/anthropic-adapter-drill.sh)
- [`scripts/provider-fallback-drill.sh`](../../../scripts/provider-fallback-drill.sh)
- [`cmd/nightlycheck/main.go`](../../../cmd/nightlycheck/main.go)
- [`cmd/nightlyreport/main.go`](../../../cmd/nightlyreport/main.go)
- [`.github/workflows/nightly-verification.yml`](../../../.github/workflows/nightly-verification.yml)
