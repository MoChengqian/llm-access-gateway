# Provider Timeout Drill

## Overview

This drill verified how the gateway behaves when the primary provider stalls long enough to exceed the adapter timeout.

The primary provider was switched to the OpenAI-compatible adapter and pointed at a local synthetic upstream that sleeps for `2.5s` before replying. The adapter timeout stayed at `1s`, and the secondary provider remained the default mock backend.

## Drill Setup

### Synthetic Upstream

```bash
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode timeout --delay-seconds 2.5
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
```

### Drill Commands

```bash
./scripts/provider-fallback-drill.sh create-fail
curl -sS -o /dev/null -w 'status=%{http_code} total=%{time_total}\n' \
  http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'
```

## Results

### Status Before The Drill

```json
{"providers":[{"name":"openai-primary","healthy":true,"consecutive_failures":0,"unhealthy_until":"0001-01-01T00:00:00Z","last_probe_at":"2026-04-07T19:50:29.800164+08:00"},{"name":"secondary","healthy":true,"consecutive_failures":0,"unhealthy_until":"0001-01-01T00:00:00Z","last_probe_at":"2026-04-07T19:50:29.80196+08:00"}],"ready":true}
```

### Request Outcome

- `POST /v1/chat/completions` still returned `200`
- the response body came from the secondary mock backend
- the observed end-to-end fallback latency from the traced drill request was `1.01681975s`
- a second timing sample taken after probe recovery measured `status=200 total=1.017395`

### Status After The Drill

```json
{"providers":[{"name":"openai-primary","healthy":false,"consecutive_failures":1,"unhealthy_until":"2026-04-07T19:51:02.075043+08:00","last_probe_at":"2026-04-07T19:50:29.800164+08:00"},{"name":"secondary","healthy":true,"consecutive_failures":0,"unhealthy_until":"0001-01-01T00:00:00Z","last_probe_at":"2026-04-07T19:50:29.80196+08:00"}],"ready":true}
```

`/readyz` stayed `200` because the secondary backend remained healthy.

## Observability Output

### Gateway Logs

```text
provider_request_failed operation=create backend=openai-primary duration=1.001339459s reason=Post "http://127.0.0.1:18081/v1/chat/completions": context deadline exceeded
provider_fallback_succeeded operation=create backend=secondary attempt=2
provider_recovered operation=probe backend=openai-primary
```

### Metrics Snapshot

The metrics snapshot was captured after the scripted drill and one extra timing sample. At that point the gateway had recorded:

```text
lag_provider_events_total{type="provider_request_failed",operation="create",backend="openai-primary"} 2
lag_provider_events_total{type="provider_fallback_succeeded",operation="create",backend="secondary"} 2
lag_provider_events_total{type="provider_skipped_unhealthy",operation="create",backend="openai-primary"} 1
lag_provider_events_total{type="provider_recovered",operation="probe",backend="openai-primary"} 1
lag_readyz_failures_total 0
```

### Upstream Observation

The synthetic upstream only logged one timed-out `POST` per failing request. That matches the current adapter behavior with a `1s` request timeout: the single timed-out attempt consumes the request context budget, so there is no remaining time for an additional HTTP retry.

## Analysis

This drill validated the gateway's timeout fallback story:

- the primary provider timed out
- the router marked it unhealthy and placed it in cooldown
- the secondary mock backend served the request
- readiness stayed green because at least one backend was still healthy

The most important nuance is that timeout behavior is bounded by the adapter's request context. `internal/provider/openai/chat.go` does support retries for timeout-like errors, but with `APP_PROVIDER_PRIMARY_TIMEOUT_SECONDS='1'` the first timeout exhausted the context budget before a second HTTP attempt could start.

## Reproduction

```bash
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode timeout --delay-seconds 2.5

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

- [Provider Error Drill](provider-errors.md)
- [Streaming Failure Drill](streaming-failures.md)
- [Routing and Resilience](../../architecture/routing-resilience.md)
- [Resilience Article](../../blog/003-resilience.md)

## Code References

- `internal/provider/openai/chat.go`
- `internal/provider/router/chat.go`
- `internal/api/handlers/chat.go`
- `internal/api/router.go`
- `scripts/provider-fallback-drill.sh`
- `scripts/synthetic-openai-upstream.py`
