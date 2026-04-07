# Streaming Failure Drill

## Overview

This report captures the two streaming failure cases that matter most for an LLM gateway:

1. failure **before** the first chunk
2. failure **after** the first chunk

Those two cases have intentionally different behavior because fallback is only safe before any response bytes are sent to the client.

## Scenario A: Failure Before The First Chunk

### Synthetic Upstream

```bash
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode stream_prechunk_fail
```

### Gateway Configuration

```bash
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'
export APP_PROVIDER_PRIMARY_TYPE='openai'
export APP_PROVIDER_PRIMARY_NAME='openai-primary'
export APP_PROVIDER_PRIMARY_BASE_URL='http://127.0.0.1:18081/v1'
export APP_PROVIDER_PRIMARY_MODEL='gpt-4o-mini'
export APP_PROVIDER_PRIMARY_TIMEOUT_SECONDS='1'
export APP_PROVIDER_PRIMARY_MAX_RETRIES='1'
export APP_PROVIDER_PRIMARY_RETRY_BACKOFF_MILLISECONDS='200'

go run ./cmd/gateway
./scripts/provider-fallback-drill.sh stream-fail
```

### Observed Client Output

The client still received a full SSE stream from the secondary mock backend, including the final `data: [DONE]` marker:

```text
HTTP/1.1 200 OK
Content-Type: text/event-stream
...
data: {"id":"chatcmpl-mock",...}
...
data: [DONE]
```

### Provider State After The Drill

```json
{"providers":[{"name":"openai-primary","healthy":false,"consecutive_failures":1,"unhealthy_until":"2026-04-07T19:53:25.59096+08:00","last_probe_at":"2026-04-07T19:52:34.651371+08:00"},{"name":"secondary","healthy":true,"consecutive_failures":0,"unhealthy_until":"0001-01-01T00:00:00Z","last_probe_at":"2026-04-07T19:52:34.652337+08:00"}],"ready":true}
```

### Observability Output

Gateway log summary:

```text
provider_request_failed operation=stream backend=openai-primary duration=202.164542ms reason=upstream status 500: synthetic stream prechunk failure
provider_fallback_succeeded operation=stream backend=secondary attempt=2
http request completed method=POST path=/v1/chat/completions status=200 duration=209.700292ms content_type=text/event-stream
```

Metrics snapshot:

```text
lag_provider_events_total{type="provider_request_failed",operation="stream",backend="openai-primary"} 1
lag_provider_events_total{type="provider_fallback_succeeded",operation="stream",backend="secondary"} 1
lag_stream_requests_total 1
lag_stream_ttft_milliseconds_count 1
lag_readyz_failures_total 0
```

The synthetic upstream logged two failing `500` responses, showing that the adapter retried once before the router moved to the secondary provider.

## Scenario B: Failure After The First Chunk

### Synthetic Upstream

```bash
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode stream_partial
```

### Client Request

```bash
curl -i -sS -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}],"stream":true}'
```

### Observed Client Output

```text
HTTP/1.1 200 OK
Content-Type: text/event-stream
...
data: {"id":"chatcmpl-upstream-1","object":"chat.completion.chunk","created":1775562915,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant","content":"partial "},"finish_reason":""}]}
```

There was no final `data: [DONE]` marker and no fallback stream from the secondary backend. The client simply received a partial SSE response and then the connection ended.

### Observability Output

Gateway log summary:

```text
provider_request_succeeded operation=stream backend=openai-primary attempt=1 duration=583.375µs
provider_stream_interrupted operation=stream backend=openai-primary attempt=1 duration=52.257ms unhealthy_until=2026-04-07T19:55:45+08:00 reason=upstream stream ended before [DONE]
provider_recovered operation=probe backend=openai-primary
```

Metrics snapshot:

```text
lag_provider_events_total{type="provider_stream_interrupted",operation="stream",backend="openai-primary"} 1
lag_provider_events_total{type="provider_recovered",operation="probe",backend="openai-primary"} 1
lag_stream_requests_total 1
lag_stream_ttft_milliseconds_count 1
lag_readyz_failures_total 0
```

Notably, there was **no** `provider_fallback_succeeded` metric in this scenario.

## Analysis

These two runs demonstrate the central streaming constraint in this gateway:

- before the first chunk, the response has not started, so the router can still switch backends safely
- after the first chunk, the response is already committed to the client, so the gateway can only surface an interrupted stream

That is why the architecture documents describe streaming fallback as a pre-first-chunk window rather than a general retry policy.

## Reproduction

```bash
# Before first chunk
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode stream_prechunk_fail
go run ./cmd/gateway
./scripts/provider-fallback-drill.sh stream-fail

# After first chunk
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode stream_partial
go run ./cmd/gateway
curl -i -sS -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}],"stream":true}'
```

## Related Documentation

- [Provider Timeout Drill](provider-timeout.md)
- [Provider Error Drill](provider-errors.md)
- [Streaming Proxy](../../architecture/streaming-proxy.md)
- [Resilience Article](../../blog/003-resilience.md)

## Code References

- `internal/provider/router/chat.go`
- `internal/provider/openai/chat.go`
- `internal/api/handlers/chat.go`
- `internal/service/chat/service.go`
- `scripts/provider-fallback-drill.sh`
- `scripts/synthetic-openai-upstream.py`
