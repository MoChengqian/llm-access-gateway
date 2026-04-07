# Quota Enforcement Drill

## Overview

This drill verified the three governance rejection paths that are visible at the HTTP edge:

- RPM rejection
- TPM rejection
- token budget rejection

All scenarios were run against the seeded `local-dev` tenant and `lag-local-dev-key`.

## Drill Setup

The drill used `docker exec` to update tenant limits in MySQL and `redis-cli FLUSHALL` to reset limiter counters between scenarios:

```bash
docker exec llm-access-gateway-redis redis-cli FLUSHALL
docker exec llm-access-gateway-mysql mysql -uuser -ppass -D llm_access_gateway -e '...'
```

The tenant was restored to its default limits after evidence collection:

- `rpm_limit=60`
- `tpm_limit=4000`
- `token_budget=1000000`

## Scenario 1: RPM Rejection

### Tenant Settings

- `rpm_limit=1`
- `tpm_limit=100`
- `token_budget=1000000`

### Commands

```bash
docker exec llm-access-gateway-redis redis-cli FLUSHALL
docker exec llm-access-gateway-mysql mysql -uuser -ppass -D llm_access_gateway \
  -e "UPDATE tenants SET rpm_limit=1, tpm_limit=100, token_budget=1000000 WHERE name='local-dev';"

curl -sS -w '\nstatus=%{http_code}\n' http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello rpm serial one"}]}'

curl -sS -w '\nstatus=%{http_code}\n' http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello rpm serial two"}]}'
```

### Observed Output

First request:

```text
{"id":"chatcmpl-upstream-3","object":"chat.completion","created":1775563020,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"Synthetic upstream response."},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}

status=200
```

Second request:

```text
{"error":"rate limit exceeded"}

status=429
```

## Scenario 2: TPM Rejection

### Tenant Settings

- `rpm_limit=100`
- `tpm_limit=5`
- `token_budget=1000000`

### Command

```bash
docker exec llm-access-gateway-redis redis-cli FLUSHALL
docker exec llm-access-gateway-mysql mysql -uuser -ppass -D llm_access_gateway \
  -e "UPDATE tenants SET rpm_limit=100, tpm_limit=5, token_budget=1000000 WHERE name='local-dev';"

curl -sS -w '\nstatus=%{http_code}\n' http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"one two three four five six"}]}'
```

### Observed Output

```text
{"error":"token rate limit exceeded"}

status=429
```

The prompt intentionally exceeded the tiny `tpm_limit=5`. The gateway estimates prompt tokens from the request text before the provider call starts, so the request was rejected without invoking the provider.

## Scenario 3: Budget Rejection

### Tenant Settings

- `rpm_limit=100`
- `tpm_limit=1000`
- `token_budget=1`

### Command

```bash
docker exec llm-access-gateway-mysql mysql -uuser -ppass -D llm_access_gateway \
  -e "UPDATE tenants SET rpm_limit=100, tpm_limit=1000, token_budget=1 WHERE name='local-dev';"

curl -sS -w '\nstatus=%{http_code}\n' http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"budget check"}]}'
```

### Observed Output

```text
{"error":"budget exceeded"}

status=403
```

## Observability Output

### Gateway Logs

The gateway logged all three rejection types at the handler boundary:

```text
method=POST path=/v1/chat/completions status=429 ... error=rate limit exceeded
method=POST path=/v1/chat/completions status=429 ... error=token rate limit exceeded
method=POST path=/v1/chat/completions status=403 ... error=budget exceeded
```

### Metrics Snapshot

```text
lag_governance_rejections_total{reason="budget_exceeded"} 1
lag_governance_rejections_total{reason="rate_limit_exceeded"} 2
lag_governance_rejections_total{reason="token_limit_exceeded"} 1
```

`rate_limit_exceeded` was `2` in this session because an earlier concurrent two-request race was also used to validate that the Redis-backed RPM limiter arbitrates correctly under contention. The serial two-request sample above is the canonical reproduction.

## Analysis

These runs confirmed three important properties:

- governance rejection happens before provider work when the request can be denied early
- RPM and TPM rejections both return `429`, but the error bodies distinguish the reason
- budget rejection returns `403`, which keeps long-term token consumption distinct from per-minute throttling

Because the limiter was backed by Redis during the drill, the rate-limit behavior reflects the production-oriented fast path instead of the MySQL fallback path.

## Reproduction

```bash
# RPM
docker exec llm-access-gateway-redis redis-cli FLUSHALL
docker exec llm-access-gateway-mysql mysql -uuser -ppass -D llm_access_gateway \
  -e "UPDATE tenants SET rpm_limit=1, tpm_limit=100, token_budget=1000000 WHERE name='local-dev';"

# TPM
docker exec llm-access-gateway-redis redis-cli FLUSHALL
docker exec llm-access-gateway-mysql mysql -uuser -ppass -D llm_access_gateway \
  -e "UPDATE tenants SET rpm_limit=100, tpm_limit=5, token_budget=1000000 WHERE name='local-dev';"

# Budget
docker exec llm-access-gateway-mysql mysql -uuser -ppass -D llm_access_gateway \
  -e "UPDATE tenants SET rpm_limit=100, tpm_limit=1000, token_budget=1 WHERE name='local-dev';"
```

## Related Documentation

- [Governance Model](../../architecture/governance.md)
- [Provider Timeout Drill](provider-timeout.md)
- [Multi-Tenant Governance](../../blog/006-multi-tenant-governance.md)

## Code References

- `internal/service/governance/service.go`
- `internal/service/governance/redis_limiter.go`
- `internal/service/governance/mysql_limiter.go`
- `internal/api/handlers/chat.go`
- `internal/store/mysql/governance_store.go`
