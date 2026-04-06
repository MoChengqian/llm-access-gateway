# Local Development

This document is the shortest path to run the current repository locally with
MySQL-backed auth, Redis-backed limiter counters, configurable provider adapters,
and provider fallback enabled.

All commands below are based on the current code and current repo layout:

- MySQL compose: `deployments/docker/docker-compose.yml`
- Redis compose: `deployments/docker/docker-compose.yml`
- local init command: `go run ./cmd/devinit`
- gateway command: `go run ./cmd/gateway`
- load test command: `go run ./cmd/loadtest`
- models endpoint: `GET /v1/models`

## Verified Local Path

The current local verification path for this repo is:

```bash
docker compose -f deployments/docker/docker-compose.yml up -d
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'
export APP_GATEWAY_PRIMARY_MOCK_FAIL_CREATE='false'
export APP_GATEWAY_PRIMARY_MOCK_FAIL_STREAM='false'
go run ./cmd/devinit
go run ./cmd/gateway
```

The expected API results are:

- missing key -> `401`
- invalid key -> `401`
- rate limit exceeded -> `429`
- token rate limit exceeded -> `429`
- budget exceeded -> `403`
- valid key -> `200`
- models list -> `200`, and the gateway still returns aggregated models if at least one provider source is healthy
- usage endpoint -> `200`, and only returns summary plus recent request usage for the authenticated tenant
- `stream:true` -> `text/event-stream` and final `[DONE]`
- if an upstream stream is interrupted after output starts, the gateway ends the stream without emitting a false `[DONE]`
- forced primary provider failure still falls back to `200`
- provider health can be inspected from `/debug/providers`
- metrics can be inspected from `/metrics`
- every response includes `X-Trace-Id` for log correlation
- primary / secondary providers can be configured as `mock` or `openai`
- provider readiness is refreshed by an active background probe loop
- OpenAI-compatible providers support timeout plus pre-stream retries before fallback

## Prerequisites

- Docker Desktop is running
- `docker` and `docker compose` are available
- Go is installed
- current working directory is the repo root:
  `/Users/luan/Desktop/llm-access-gateway`

## Optional: Full Container Stack

If you want the whole stack in containers instead of running the gateway from your shell:

```bash
docker compose -f deployments/docker/docker-compose.yml up -d --build
docker compose -f deployments/docker/docker-compose.yml ps
docker compose -f deployments/docker/docker-compose.yml logs devinit
curl -i http://127.0.0.1:8080/healthz
```

Expected response:

```text
NAME                           IMAGE                      STATUS
llm-access-gateway-mysql       mysql:8.4                  healthy
llm-access-gateway-redis       redis:7.4-alpine           healthy
llm-access-gateway-devinit     llm-access-gateway:latest  exited (0)
llm-access-gateway             llm-access-gateway:latest  healthy
```

For the health check request, you should see `HTTP/1.1 200 OK` plus `X-Request-Id` and `X-Trace-Id`.

## 1. Start Docker MySQL

Run:

```bash
docker compose -f deployments/docker/docker-compose.yml up -d
```

Expected output:

```text
[+] Running ...
 ✔ Container llm-access-gateway-mysql Started
```

The compose file creates:

- database: `llm_access_gateway`
- user: `user`
- password: `pass`
- port: `3306`
- redis port: `6379`

## 2. Wait for MySQL Ready

Run:

```bash
until [ "$(docker inspect -f '{{.State.Health.Status}}' llm-access-gateway-mysql)" = "healthy" ]; do
  sleep 1
done

docker inspect -f '{{.State.Health.Status}}' llm-access-gateway-mysql

until [ "$(docker inspect -f '{{.State.Health.Status}}' llm-access-gateway-redis)" = "healthy" ]; do
  sleep 1
done

docker inspect -f '{{.State.Health.Status}}' llm-access-gateway-redis
```

Expected output:

```text
healthy
```

If you want to inspect container state:

```bash
docker ps --filter name=llm-access-gateway-mysql
docker logs llm-access-gateway-mysql
```

## 3. Configure APP_MYSQL_DSN

Run:

```bash
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'
```

Expected output:

```text
# no output
```

This DSN matches the compose file exactly.

## 3.1 Optional: Configure a Real OpenAI-Compatible Upstream

If you want the gateway to proxy to a real OpenAI-compatible provider instead of the built-in mock primary backend, export:

```bash
export APP_PROVIDER_PRIMARY_TYPE='openai'
export APP_PROVIDER_PRIMARY_BASE_URL='https://api.openai.com/v1'
export APP_PROVIDER_PRIMARY_API_KEY='sk-...'
export APP_PROVIDER_PRIMARY_MODEL='gpt-4.1-mini'
export APP_PROVIDER_PRIMARY_TIMEOUT_SECONDS='15'
export APP_PROVIDER_PRIMARY_MAX_RETRIES='1'
export APP_PROVIDER_PRIMARY_RETRY_BACKOFF_MILLISECONDS='200'
```

Notes:

- `APP_PROVIDER_PRIMARY_BASE_URL` should point to an upstream base path that already includes `/v1`
- the secondary backend can stay on the default `mock` type for local fallback verification
- if you do not set these variables, the repo keeps the current mock-first behavior
- timeout applies to non-stream requests and provider probes; stream retries only happen before the upstream stream opens

## 3.2 Optional: Run the Built-In Load Test

After the gateway is up and `lag-local-dev-key` is seeded, you can run:

```bash
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 20 -concurrency 4
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 10 -concurrency 2 -stream
```

Expected output includes:

- `success=` and `failure=` counts
- `status_counts=200=...`
- `latency_p50=... latency_p95=...`
- for stream mode: `stream_chunks_total=` and `ttft_p50=...`

## 4. Initialize Local Tenant and API Key

Run:

```bash
go run ./cmd/devinit
```

Expected output:

```text
development auth seed ready
tenant=local-dev
api_key=lag-local-dev-key
rpm_limit=60
tpm_limit=4000
token_budget=1000000
```

What this command does:

- ensures the `tenants` table exists
- ensures the `api_keys` table exists
- ensures the `request_usages` table exists
- creates or updates tenant `local-dev`
- creates or updates one valid API key: `lag-local-dev-key`
- sets tenant `local-dev` to `60 req/min`
- sets tenant `local-dev` to `4000 tokens/min`
- sets tenant `local-dev` to `1000000 token budget`

## 5. Start the Gateway

In a separate terminal, still at repo root, run:

```bash
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'
export APP_GATEWAY_PRIMARY_MOCK_FAIL_CREATE='false'
export APP_GATEWAY_PRIMARY_MOCK_FAIL_STREAM='false'
go run ./cmd/gateway
```

Expected output:

```text
INFO gateway starting address=:8080
```

If startup succeeds, the gateway is listening on:

```text
http://127.0.0.1:8080
```

## 6. Verify Missing API Key

Run:

```bash
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'
```

Expected response:

```text
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer
...
{"error":"missing api key"}
```

## 7. Verify Invalid API Key

Run:

```bash
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer invalid-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'
```

Expected response:

```text
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer
...
{"error":"invalid api key"}
```

## 8. Verify Valid API Key

Run:

```bash
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'
```

Expected response:

```text
HTTP/1.1 200 OK
Content-Type: application/json
...
{"id":"chatcmpl-mock","object":"chat.completion",...}
```

The body should contain mock assistant content from the current provider
implementation.

## 9. Verify SSE Streaming

Run:

```bash
curl -i -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}],"stream":true}'
```

Expected response:

```text
HTTP/1.1 200 OK
Content-Type: text/event-stream
...
data: {"id":"chatcmpl-mock","object":"chat.completion.chunk",...}

data: [DONE]
```

The key checks for streaming are:

- response header includes `Content-Type: text/event-stream`
- response contains multiple `data:` events
- response ends with `data: [DONE]`

## 9.1 Verify Tenant Usage Summary

Run:

```bash
curl -i 'http://127.0.0.1:8080/v1/usage?limit=5' \
  -H 'Authorization: Bearer lag-local-dev-key'
```

Expected response:

```text
HTTP/1.1 200 OK
Content-Type: application/json
...
{"object":"usage","tenant":{"id":...,"name":"local-dev"},"summary":{"requests_last_minute":...,"tokens_last_minute":...,"total_tokens_used":...},"data":[...]}
```

The key checks are:

- response header includes `Content-Type: application/json`
- body contains `"object":"usage"`
- body contains tenant quota fields such as `rpm_limit`, `tpm_limit`, and `token_budget`
- body contains recent request records in `data`

## 10. Verify Provider Fallback

To force the primary mock provider to fail for non-stream requests:

```bash
export APP_GATEWAY_PRIMARY_MOCK_FAIL_CREATE='true'
export APP_GATEWAY_PRIMARY_MOCK_FAIL_STREAM='false'
go run ./cmd/gateway
```

Then call:

```bash
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'
```

Expected response:

```text
HTTP/1.1 200 OK
Content-Type: application/json
...
{"id":"chatcmpl-mock","object":"chat.completion",...}
```

Then inspect provider health:

```bash
curl -i http://127.0.0.1:8080/debug/providers
curl -i http://127.0.0.1:8080/readyz
curl -i http://127.0.0.1:8080/metrics
curl -i http://127.0.0.1:8080/healthz
```

Expected response:

```text
HTTP/1.1 200 OK
Content-Type: application/json
...
{"ready":true,"providers":[{"name":"mock-primary","healthy":false,...
```

For `curl -i http://127.0.0.1:8080/healthz`, you should also see:

```text
X-Request-Id: ...
X-Trace-Id: ...
```

For `curl -i http://127.0.0.1:8080/metrics`, you should see gateway counters including:

```text
lag_governance_rejections_total{reason="rate_limit_exceeded"} ...
lag_stream_requests_total ...
lag_stream_chunks_total ...
lag_stream_ttft_milliseconds_count ...
lag_provider_operation_duration_milliseconds_count{operation="create",backend="primary",result="success"} ...
lag_provider_probe_results_total{backend="primary",result="success"} ...
```

You can also use the helper script:

```bash
./scripts/provider-fallback-drill.sh create-fail
./scripts/gateway-smoke-check.sh
```

To force the primary mock provider to fail before streaming starts:

```bash
export APP_GATEWAY_PRIMARY_MOCK_FAIL_CREATE='false'
export APP_GATEWAY_PRIMARY_MOCK_FAIL_STREAM='true'
go run ./cmd/gateway
```

Then call:

```bash
curl -i -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}],"stream":true}'
```

Expected response:

```text
HTTP/1.1 200 OK
Content-Type: text/event-stream
...
data: {"id":"chatcmpl-mock","object":"chat.completion.chunk",...}

data: [DONE]
```

If both providers are forced into failure and still in cooldown, `readyz`
returns:

```text
HTTP/1.1 503 Service Unavailable
...
{"status":"not ready"}
```

You can also use:

```bash
./scripts/provider-fallback-drill.sh stream-fail
```

## 11. Optional Cleanup

Stop the gateway with `Ctrl+C`.

Stop MySQL:

```bash
docker compose -f deployments/docker/docker-compose.yml down
```

If you want to remove persisted database data too:

```bash
docker compose -f deployments/docker/docker-compose.yml down -v
```

## Common Errors

### 3306 port occupied

Symptom:

```text
Bind for 0.0.0.0:3306 failed
```

Check:

```bash
lsof -nP -iTCP:3306 -sTCP:LISTEN
```

Fix:

- stop the process already using `3306`
- or stop your local MySQL if it is already running
- then rerun:

```bash
docker compose -f deployments/docker/docker-compose.yml up -d
```

### 8080 port occupied

Symptom:

```text
listen tcp :8080: bind: address already in use
```

Check:

```bash
lsof -nP -iTCP:8080 -sTCP:LISTEN
```

Fix option 1:

- stop the existing process using `8080`

Fix option 2:

```bash
export APP_SERVER_ADDRESS='127.0.0.1:18080'
go run ./cmd/gateway
```

Then use `http://127.0.0.1:18080` in your curl commands.

### connection refused

Common causes:

- MySQL container is not ready yet
- gateway is not running
- wrong port

Checks:

```bash
docker inspect -f '{{.State.Health.Status}}' llm-access-gateway-mysql
lsof -nP -iTCP:8080 -sTCP:LISTEN
```

Fix:

- wait until MySQL is `healthy`
- rerun `go run ./cmd/devinit`
- restart `go run ./cmd/gateway`

### access denied

Symptom:

```text
Error 1045 (28000): Access denied
```

Cause:

- `APP_MYSQL_DSN` does not match the compose credentials

Use the exact DSN below:

```bash
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
```

If you changed the compose credentials, update the DSN to match.

### `.zshrc` 报错与项目无关

You may see shell startup noise like:

```text
/Users/luan/.zshrc:source:17: no such file or directory: ...
```

This is a local shell initialization issue. It is not caused by the
`llm-access-gateway` repo itself.

If the command still runs afterward, you can ignore it for this project.
