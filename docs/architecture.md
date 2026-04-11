# Architecture

This document describes the current repository architecture as implemented
today. It is intentionally practical and maps directly to the current code.

## System Position

`LLM Access Gateway` sits between client applications and upstream model
providers.

```text
Client / App
    |
    v
LLM Access Gateway
    |
    +--> primary provider
    |
    +--> secondary provider
```

The gateway is responsible for:

- unified OpenAI-compatible HTTP entrypoints
- API key authentication and tenant resolution
- RPM / TPM / budget checks
- SSE proxying
- provider routing, fallback, and health state
- metrics, tracing, and structured logs
- local and basic deployment support

It is not responsible for:

- model training
- inference optimization
- RAG or vector retrieval
- frontend UI
- agent workflow

## Main Request Flow

For `POST /v1/chat/completions`, the current path is:

```text
router
  -> request_id middleware
  -> trace middleware
  -> auth middleware
  -> chat handler
  -> governance service
  -> chat service
  -> provider router
  -> provider backend
```

More concretely:

- [router.go](/Users/luan/Desktop/llm-access-gateway/internal/api/router.go)
  wires request middleware, auth checks, and endpoint handlers
- [service.go](/Users/luan/Desktop/llm-access-gateway/internal/auth/service.go)
  resolves `Authorization: Bearer <key>` into a tenant-scoped principal
- [service.go](/Users/luan/Desktop/llm-access-gateway/internal/service/governance/service.go)
  performs budget checks, asks the limiter for admission, and creates request usage records
- [service.go](/Users/luan/Desktop/llm-access-gateway/internal/service/chat/service.go)
  validates request shape and normalizes gateway responses
- [chat.go](/Users/luan/Desktop/llm-access-gateway/internal/provider/router/chat.go)
  handles provider selection, passive health, fallback, and probes
- provider implementations live under [provider](/Users/luan/Desktop/llm-access-gateway/internal/provider)

## Current Modules

### API Layer

Current endpoints:

- `GET /healthz`
- `GET /readyz`
- `GET /debug/providers`
- `GET /metrics`
- `GET /v1/models`
- `GET /v1/usage`
- `POST /v1/chat/completions`

Handlers are intentionally thin. They parse HTTP concerns and delegate business
logic to services.

### Auth Layer

The auth layer:

- extracts bearer tokens
- hashes the presented API key with SHA-256
- looks up the key in MySQL
- resolves the tenant
- stores a tenant-scoped principal in request context

The gateway does not store raw API keys in MySQL.

### Governance Layer

The governance layer currently handles:

- RPM limiting
- TPM limiting
- token budget checks
- request usage record creation and completion

Limiter counters are Redis-backed when Redis is available, with MySQL fallback
for local resilience.

Usage records are stored in MySQL.

### Provider Layer

The provider layer currently includes:

- configurable `mock` backends
- configurable OpenAI-compatible backends
- primary / secondary routing
- active probing
- passive health and cooldown
- fallback before the first stream chunk only
- pre-stream retries for OpenAI-compatible upstreams

### Observability Layer

Current observability defaults include:

- `X-Request-Id` on responses
- `X-Trace-Id` on responses
- request -> handler -> provider trace correlation
- optional OTLP/HTTP trace export
- structured access logs
- Prometheus-style metrics on `/metrics`
- Grafana dashboard asset for the `/metrics` contract

### Delivery Layer

Current delivery support includes:

- `Dockerfile`
- local Docker Compose stack
- baseline Kubernetes manifests
- local `devinit` bootstrap command
- built-in smoke and load test helpers

## Data Stores

### MySQL

MySQL stores:

- `tenants`
- `api_keys`
- `request_usages`

### Redis

Redis stores short-lived counter state for:

- RPM checks
- TPM checks

## Design Boundaries

Several boundaries are explicit in the current codebase:

- handlers stay thin
- auth happens before business handlers run
- governance happens before provider invocation
- stream fallback is only allowed before the first chunk is emitted
- request IDs and trace IDs are preserved across new request paths

## Current Limitations

The repository is still intentionally small. Current limitations include:

- no RAG or retrieval layer
- no admin UI
- no tenant management API
- no long-term analytics store beyond MySQL request usage records
- no production-grade distributed tracing backend yet
- no provider-specific quota management beyond the gateway's own limits
