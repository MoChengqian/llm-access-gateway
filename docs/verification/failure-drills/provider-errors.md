# Provider Error Drill

## Overview

This drill verified how the gateway handles upstream `5xx` responses from the primary provider.

The primary provider used the OpenAI-compatible adapter and pointed at a local synthetic upstream that always returns `500`. The secondary provider remained the default mock backend.

## Drill Setup

### Synthetic Upstream

```bash
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode error500
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
./scripts/provider-fallback-drill.sh create-fail
```

## Results

### Status Before The Drill

```json
{"providers":[{"name":"openai-primary","healthy":true,"consecutive_failures":0,"unhealthy_until":"0001-01-01T00:00:00Z","last_probe_at":"2026-04-07T19:51:48.254674+08:00"},{"name":"secondary","healthy":true,"consecutive_failures":0,"unhealthy_until":"0001-01-01T00:00:00Z","last_probe_at":"2026-04-07T19:51:48.255725+08:00"}],"ready":true}
```

### Request Outcome

- `POST /v1/chat/completions` returned `200`
- the final JSON body came from the secondary mock backend
- the traced request completed in `213.798209ms`
- the router placed `openai-primary` into cooldown immediately after the failure

### Status After The Drill

```json
{"providers":[{"name":"openai-primary","healthy":false,"consecutive_failures":1,"unhealthy_until":"2026-04-07T19:52:24.268+08:00","last_probe_at":"2026-04-07T19:51:48.254674+08:00"},{"name":"secondary","healthy":true,"consecutive_failures":0,"unhealthy_until":"0001-01-01T00:00:00Z","last_probe_at":"2026-04-07T19:51:48.255725+08:00"}],"ready":true}
```

## Observability Output

### Gateway Logs

```text
provider_request_failed operation=create backend=openai-primary duration=202.14875ms reason=upstream status 500: synthetic upstream 500
provider_fallback_succeeded operation=create backend=secondary attempt=2
http request completed method=POST path=/v1/chat/completions status=200 duration=213.798209ms
```

### Upstream Observation

The synthetic upstream logged **two** failing `POST /v1/chat/completions` calls for the same client request:

```text
chat mode=error500 stream=False
"POST /v1/chat/completions HTTP/1.1" 500 -
chat mode=error500 stream=False
"POST /v1/chat/completions HTTP/1.1" 500 -
```

That is the important distinction from the timeout drill. For `500` responses, the adapter had enough request budget to perform one retry before the router marked the backend unhealthy and switched to the secondary provider.

### Metrics Snapshot

```text
lag_provider_events_total{type="provider_request_failed",operation="create",backend="openai-primary"} 1
lag_provider_events_total{type="provider_fallback_succeeded",operation="create",backend="secondary"} 1
lag_readyz_failures_total 0
```

## Analysis

This drill validated both resilience layers:

- adapter-level retry inside `internal/provider/openai/chat.go`
- router-level fallback inside `internal/provider/router/chat.go`

The adapter retried the upstream `500` once, then the router marked the backend unhealthy and served the request from the secondary mock backend. Readiness stayed green because one backend was still healthy.

## Reproduction

```bash
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode error500

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
./scripts/provider-fallback-drill.sh create-fail
```

## Related Documentation

- [Provider Timeout Drill](provider-timeout.md)
- [Streaming Failure Drill](streaming-failures.md)
- [Provider Adapters](../../architecture/provider-adapters.md)
- [Resilience Article](../../blog/003-resilience.md)

## Code References

- `internal/provider/openai/chat.go`
- `internal/provider/router/chat.go`
- `internal/obs/metrics/registry.go`
- `scripts/provider-fallback-drill.sh`
- `scripts/synthetic-openai-upstream.py`
