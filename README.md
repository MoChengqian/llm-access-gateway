# LLM Access Gateway

LLM Access Gateway is a Go service that exposes an OpenAI-compatible
`POST /v1/chat/completions` and `GET /v1/models` APIs with:

- API key authentication backed by MySQL
- Redis-backed RPM / TPM limiting with MySQL fallback
- tenant resolution from API key
- provider routing with configurable OpenAI-compatible or mock backends
- active provider probing via the models endpoint
- mock non-streaming chat completions
- SSE streaming chat completions
- provider health and debug status endpoints
- request ID propagation in responses and logs

The current codebase is intentionally small. It is suitable for local
development, auth validation, and API contract verification before adding
real provider adapters, richer routing, or production observability.

## Core Capabilities

- `POST /v1/chat/completions`
- `GET /v1/models`
- `Authorization: Bearer <key>` authentication
- MySQL-backed tenant and API key lookup
- `stream=false` JSON response
- `stream=true` SSE response with `Content-Type: text/event-stream`
- final streaming marker `data: [DONE]`
- health endpoints: `GET /healthz` and `GET /readyz`
- provider status endpoint: `GET /debug/providers`
- metrics endpoint: `GET /metrics`
- tracing header: `X-Trace-Id` on every HTTP response
- configurable OpenAI-compatible upstream provider adapter

## Project Structure

```text
cmd/
  devinit/     Initialize local MySQL schema and seed one development tenant/key
  gateway/     Start the HTTP gateway
  loadtest/    Run a built-in load test against chat completions

configs/
  config.yaml  Default app configuration

deployments/
  docker/
    docker-compose.yml  Local MySQL, Redis, devinit, and gateway stack
  k8s/
    *.yaml      Namespace, ConfigMap, Secret example, Deployment, Service

docs/
  local-development.md  Step-by-step local setup and troubleshooting

internal/
  api/         Router and HTTP handlers
  auth/        Bearer auth and tenant resolution
  config/      Config loading
  provider/    Provider interface and mock provider
  provider/router Failover router with passive backend health
  service/chat Chat request validation and response shaping
  store/mysql/ MySQL auth lookup, usage storage, and local bootstrap helpers
  store/redis/ Minimal Redis client for limiter counters

migrations/
  001_init.sql Initial tenants/api_keys schema
```

## Quick Start

The quickest local path is:

```bash
docker compose -f deployments/docker/docker-compose.yml up -d

until [ "$(docker inspect -f '{{.State.Health.Status}}' llm-access-gateway-mysql)" = "healthy" ]; do
  sleep 1
done

until [ "$(docker inspect -f '{{.State.Health.Status}}' llm-access-gateway-redis)" = "healthy" ]; do
  sleep 1
done

export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'

go run ./cmd/devinit
go run ./cmd/gateway
```

Expected output:

```text
# go run ./cmd/devinit
development auth seed ready
tenant=local-dev
api_key=lag-local-dev-key
rpm_limit=60
tpm_limit=4000
token_budget=1000000

# go run ./cmd/gateway
INFO gateway starting address=:8080
```

For a full walkthrough, see [docs/local-development.md](docs/local-development.md).

## Container Quick Start

To start the full local stack in containers:

```bash
docker compose -f deployments/docker/docker-compose.yml up -d --build
docker compose -f deployments/docker/docker-compose.yml ps
docker compose -f deployments/docker/docker-compose.yml logs devinit
curl -i http://127.0.0.1:8080/healthz
```

Expected result:

- `mysql`, `redis`, and `gateway` become healthy
- `devinit` exits with code `0`
- `curl` returns `200` plus `X-Request-Id` and `X-Trace-Id`

## K8s Basics

The repo now includes baseline Kubernetes manifests in [deployments/k8s](deployments/k8s):

- `namespace.yaml`
- `configmap.yaml`
- `secret.example.yaml`
- `job.yaml`
- `deployment.yaml`
- `service.yaml`

Apply them in this order after replacing the MySQL DSN in `secret.example.yaml`:

```bash
kubectl apply -f deployments/k8s/namespace.yaml
kubectl apply -f deployments/k8s/configmap.yaml
kubectl apply -f deployments/k8s/secret.example.yaml
kubectl apply -f deployments/k8s/job.yaml
kubectl -n llm-access-gateway wait --for=condition=complete job/llm-access-gateway-devinit --timeout=120s
kubectl apply -f deployments/k8s/deployment.yaml
kubectl apply -f deployments/k8s/service.yaml
kubectl -n llm-access-gateway get pods,svc
```

The deployment uses:

- `/healthz` as liveness probe
- `/readyz` as readiness probe
- `APP_*` environment variables from ConfigMap and Secret
- port `8080` for both API and `/metrics`

## Auth

The chat completions endpoint requires:

```text
Authorization: Bearer <api-key>
```

Current auth behavior:

- missing `Authorization` -> `401` with `{"error":"missing api key"}`
- invalid key -> `401` with `{"error":"invalid api key"}`
- disabled key or disabled tenant -> `401`
- tenant RPM limit exceeded -> `429` with `{"error":"rate limit exceeded"}`
- tenant TPM limit exceeded -> `429` with `{"error":"token rate limit exceeded"}`
- tenant token budget exceeded -> `403` with `{"error":"budget exceeded"}`
- valid key -> request continues to chat service

For local development, `go run ./cmd/devinit` seeds:

- tenant: `local-dev`
- API key: `lag-local-dev-key`
- tenant RPM limit: `60 req/min`
- tenant TPM limit: `4000 tokens/min`
- tenant token budget: `1000000 tokens`

The gateway stores and looks up the SHA-256 hash of the API key. It does not
store raw API keys in MySQL.

## API Quick Checks

After `go run ./cmd/devinit` and `go run ./cmd/gateway`, you can use this
single-screen checklist directly:

```bash
# missing key -> 401
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'

# invalid key -> 401
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer invalid-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'

# valid key, non-stream -> 200 JSON
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'

# valid key, models -> 200 JSON list
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer lag-local-dev-key'

# valid key, stream -> text/event-stream + [DONE]
curl -i -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}],"stream":true}'
```

Expected results:

- missing key -> `401` and `{"error":"missing api key"}`
- invalid key -> `401` and `{"error":"invalid api key"}`
- valid key -> `200` with `"object":"chat.completion"`
- models -> `200` with `"object":"list"`
- `stream:true` -> `Content-Type: text/event-stream` and final `data: [DONE]`
- with Redis enabled, RPM / TPM counters are enforced from Redis first and fall back to MySQL if Redis is unavailable
- if the primary mock provider fails before any response is produced, the secondary mock provider is used automatically
- `GET /debug/providers` shows backend health, failure count, and cooldown state
- `GET /metrics` exposes request count, provider failures, fallback count, and readyz failures
- `/metrics` also exposes governance rejection counts plus stream request, chunk, and TTFT counters
- every request returns `X-Trace-Id`, and logs now include `trace_id`, `span_id`, and provider span events for request -> handler -> provider correlation

## Local Development Entry

Common local entry points:

```bash
go run ./cmd/devinit
go run ./cmd/gateway
make test
make fmt
make loadtest
```

Environment variables currently used by the code:

```bash
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'
export APP_SERVER_ADDRESS=':8080'
export APP_GATEWAY_PRIMARY_MOCK_FAIL_CREATE='false'
export APP_GATEWAY_PRIMARY_MOCK_FAIL_STREAM='false'
export APP_GATEWAY_PROVIDER_PROBE_INTERVAL_SECONDS='30'
export APP_PROVIDER_PRIMARY_TYPE='mock'
export APP_PROVIDER_PRIMARY_BASE_URL=''
export APP_PROVIDER_PRIMARY_API_KEY=''
export APP_PROVIDER_PRIMARY_MODEL=''
export APP_PROVIDER_SECONDARY_TYPE='mock'
export APP_PROVIDER_SECONDARY_BASE_URL=''
export APP_PROVIDER_SECONDARY_API_KEY=''
export APP_PROVIDER_SECONDARY_MODEL=''
```

To use a real OpenAI-compatible upstream as the primary backend:

```bash
export APP_PROVIDER_PRIMARY_TYPE='openai'
export APP_PROVIDER_PRIMARY_BASE_URL='https://api.openai.com/v1'
export APP_PROVIDER_PRIMARY_API_KEY='sk-...'
export APP_PROVIDER_PRIMARY_MODEL='gpt-4.1-mini'
```

The gateway will keep the configured secondary backend for fallback, and streaming fallback still only happens before the first chunk is emitted.
Provider readiness is also refreshed by a background probe loop that uses the configured provider model listing path.

## Load Testing

The repo includes a built-in load test command so you can get a quick baseline without external tools:

```bash
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 20 -concurrency 4
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 10 -concurrency 2 -stream
```

Expected output includes:

- total request count and concurrency
- success / failure counts
- status code distribution
- latency p50 / p95 / max
- for streaming: TTFT p50 / p95 / max and total streamed chunk count

For local fallback verification you can temporarily force the primary mock
provider to fail before a response starts:

```bash
export APP_GATEWAY_PRIMARY_MOCK_FAIL_CREATE='true'
go run ./cmd/gateway

export APP_GATEWAY_PRIMARY_MOCK_FAIL_STREAM='true'
go run ./cmd/gateway
```

Expected result:

- non-stream requests still return `200`
- stream requests still return `text/event-stream` and final `data: [DONE]`
- `curl -i http://127.0.0.1:8080/readyz` returns `503` when all providers are in cooldown
- `curl -i http://127.0.0.1:8080/debug/providers` shows which backend is unhealthy

There is also a small drill helper:

```bash
./scripts/provider-fallback-drill.sh status
./scripts/provider-fallback-drill.sh create-fail
./scripts/provider-fallback-drill.sh stream-fail
./scripts/gateway-smoke-check.sh
```

Default config file: [`configs/config.yaml`](configs/config.yaml)

## Common Questions

### Why does `go run ./cmd/gateway` fail with `mysql dsn is required`?

Because the gateway now requires MySQL-backed auth on startup. Export
`APP_MYSQL_DSN` before starting the service.

### How do I create a valid local API key?

Run:

```bash
go run ./cmd/devinit
```

It will create or update one development tenant and one valid key:
`lag-local-dev-key`.

### Why do I get `401 missing api key`?

Your request is missing the `Authorization: Bearer <key>` header.

### Why do I get `401 invalid api key`?

The key was not found in MySQL. For a known-good local key, run
`go run ./cmd/devinit` and use `lag-local-dev-key`.

### Where is the full local setup guide?

See [docs/local-development.md](docs/local-development.md).
