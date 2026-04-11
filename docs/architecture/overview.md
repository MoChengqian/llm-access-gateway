# Architecture Overview

## Introduction

The LLM Access Gateway is a multi-tenant model access and governance gateway written in Go. It sits between client applications and upstream LLM providers, providing a unified OpenAI-compatible API with authentication, rate limiting, routing, and observability.

This document provides a high-level overview of the system architecture, explaining the major layers, component boundaries, and design philosophy.

## System Position

The gateway occupies a specific position in the LLM ecosystem:

```text
┌─────────────────┐
│ Client / App    │
└────────┬────────┘
         │
         │ HTTP (OpenAI-compatible API)
         │
         ▼
┌─────────────────────────────────────┐
│   LLM Access Gateway                │
│                                     │
│  ┌──────────────────────────────┐  │
│  │  API Layer                   │  │
│  │  /v1/chat/completions        │  │
│  │  /v1/models                  │  │
│  │  /v1/usage                   │  │
│  └──────────────────────────────┘  │
│                                     │
│  ┌──────────────────────────────┐  │
│  │  Auth & Governance Layer     │  │
│  │  API Key → Tenant            │  │
│  │  RPM / TPM / Budget          │  │
│  └──────────────────────────────┘  │
│                                     │
│  ┌──────────────────────────────┐  │
│  │  Provider Routing Layer      │  │
│  │  Health Tracking             │  │
│  │  Fallback Logic              │  │
│  └──────────────────────────────┘  │
│                                     │
│  ┌──────────────────────────────┐  │
│  │  Observability Layer         │  │
│  │  Metrics / Tracing / Logs    │  │
│  └──────────────────────────────┘  │
└─────────────────────────────────────┘
         │
         │ HTTP (OpenAI-compatible)
         │
         ▼
┌─────────────────────────────────────┐
│   Upstream Providers                │
│   ┌──────────┐    ┌──────────┐     │
│   │ Primary  │    │Secondary │     │
│   │ Provider │    │ Provider │     │
│   └──────────┘    └──────────┘     │
└─────────────────────────────────────┘
```

## What the Gateway Does

The gateway is responsible for:

- **Unified API**: Exposing OpenAI-compatible HTTP endpoints (`POST /v1/chat/completions`, `GET /v1/models`, `GET /v1/usage`)
- **Authentication**: API key-based authentication with tenant resolution
- **Governance**: Multi-tenant rate limiting (RPM/TPM) and token budget enforcement
- **SSE Streaming Proxy**: Proxying Server-Sent Events streams from providers to clients
- **Provider Routing**: Selecting between multiple provider backends with health tracking
- **Resilience**: Retry logic and fallback to secondary providers (before first chunk only for streams)
- **Observability**: Request/trace ID propagation, structured logging, Prometheus metrics, log-based trace correlation, optional OTLP trace export
- **Deployment Support**: Docker Compose and Kubernetes deployment configurations

## What the Gateway Does Not Do

The gateway explicitly does not handle:

- **Model Training**: No training or fine-tuning capabilities
- **Inference Optimization**: No model serving or inference acceleration
- **RAG or Vector Retrieval**: No retrieval-augmented generation or vector database features
- **Frontend UI**: No web interface or dashboard
- **Agent Workflow**: No multi-step agent orchestration
- **Model Fine-Tuning**: No model customization or adaptation

The gateway is focused on access control, governance, and routing—not on the models themselves.

## System Layers

### 1. API Layer

**Location**: `internal/api/`

The API layer handles HTTP concerns:
- Request routing and middleware chain
- Request ID generation and propagation
- Request body size limits
- HTTP error responses

**Key Components**:
- `router.go`: Chi-based HTTP router with middleware chain
- `handlers/`: Thin HTTP handlers that delegate to services

**Boundaries**:
- Handlers stay thin and focus on HTTP concerns only
- Business logic lives in service layer, not handlers
- All handlers return standardized JSON error responses

### 2. Authentication Layer

**Location**: `internal/auth/`

The authentication layer resolves API keys to tenants:
- Extracts `Authorization: Bearer <key>` header
- Hashes the API key with SHA-256
- Looks up the hashed key in MySQL
- Resolves the associated tenant
- Stores a tenant-scoped principal in request context

**Key Components**:
- `service.go`: Authenticator implementation

**Boundaries**:
- Raw API keys are never stored in the database
- Only SHA-256 hashes are persisted
- Authentication happens before any business logic
- Invalid/missing/disabled keys result in 401 responses

### 3. Governance Layer

**Location**: `internal/service/governance/`

The governance layer enforces multi-tenant quotas:
- **RPM (Requests Per Minute)**: Rate limit on request count
- **TPM (Tokens Per Minute)**: Rate limit on token consumption
- **Token Budget**: Long-term token budget enforcement
- **Usage Tracking**: Records request usage in MySQL

**Key Components**:
- `service.go`: Governance service orchestration
- `redis_limiter.go`: Redis-backed rate limiting
- `mysql_limiter.go`: MySQL fallback rate limiting

**Boundaries**:
- Governance checks happen after auth but before provider invocation
- Redis is used for counters when available, with MySQL fallback
- Usage records are created before requests and completed after responses
- Quota violations result in 429 (rate limit) or 403 (budget) responses

### 4. Business Service Layer

**Location**: `internal/service/`

The service layer contains business logic:
- **Chat Service** (`service/chat/`): Request validation and response normalization
- **Models Service** (`service/models/`): Model listing aggregation
- **Usage Service** (`service/usage/`): Usage query and reporting

**Boundaries**:
- Services contain business logic, not HTTP concerns
- Services call provider layer for upstream communication
- Services return domain errors, not HTTP status codes

### 5. Provider Layer

**Location**: `internal/provider/`

The provider layer abstracts upstream model providers:
- **Provider Interface**: Unified `ChatCompletionProvider` and `ModelProvider` interfaces
- **Mock Provider** (`provider/mock/`): In-memory mock for testing
- **OpenAI Provider** (`provider/openai/`): OpenAI-compatible HTTP client
- **Anthropic Provider** (`provider/anthropic/`): Anthropic Messages API adapter
- **Ollama Provider** (`provider/ollama/`): Ollama local HTTP API adapter
- **Provider Router** (`provider/router/`): Health tracking, routing, fallback, and probing

**Key Components**:
- `provider.go`: Provider interface definitions
- `router/chat.go`: Provider selection and fallback logic
- `anthropic/chat.go`: Anthropic provider adapter
- `mock/chat.go`: Mock provider implementation
- `openai/chat.go`: OpenAI-compatible provider adapter
- `ollama/chat.go`: Ollama provider adapter

**Boundaries**:
- All providers implement the same interface
- Provider-specific details are hidden behind the interface
- Anthropic system prompts are translated from OpenAI-style `system` messages into Anthropic's top-level `system` field inside the adapter
- Fallback only happens before the first stream chunk is sent
- Health state is tracked passively based on request outcomes
- Active probing refreshes provider health periodically

### 6. Observability Layer

**Location**: `internal/obs/`

The observability layer provides visibility into system behavior:
- **Metrics** (`obs/metrics/`): Prometheus-style metrics on `/metrics`
- **Tracing** (`obs/tracing/`): Request and trace ID propagation
- **Logging**: Structured logs with request/trace correlation

**Key Observability Features**:
- Every request gets a unique `X-Request-Id`
- Every request gets a unique `X-Trace-Id` for request/trace correlation
- Logs include `request_id`, `trace_id`, `span_id`, and tenant context
- Metrics track request counts, latencies, provider failures, and governance rejections
- Traces correlate request → handler → provider spans

**Boundaries**:
- Request IDs are generated at the router level
- Trace IDs are propagated through context
- Metrics are recorded via middleware and service calls
- Logs use structured fields for machine parsing

### 7. Data Store Layer

**Location**: `internal/store/`

The data store layer manages persistence:
- **MySQL** (`store/mysql/`): Tenants, API keys, request usage records
- **Redis** (`store/redis/`): Short-lived rate limit counters

**Boundaries**:
- MySQL is the source of truth for auth and usage data
- Redis is used for performance but is optional (MySQL fallback exists)
- Stores expose domain-specific interfaces, not raw SQL

## Request Flow

A typical request flows through these layers:

```text
1. HTTP Request arrives
   ↓
2. Router applies middleware:
   - Request ID generation
   - Trace ID generation
   - Request/Trace ID headers added to response
   ↓
3. Auth middleware:
   - Extract Authorization header
   - Hash API key
   - Look up tenant in MySQL
   - Store principal in context
   ↓
4. Handler receives request:
   - Parse request body
   - Validate request shape
   ↓
5. Governance service:
   - Check token budget
   - Check RPM limit (Redis or MySQL)
   - Check TPM limit (Redis or MySQL)
   - Create usage record
   ↓
6. Chat service:
   - Validate request
   - Call provider router
   ↓
7. Provider router:
   - Select healthy provider
   - Invoke provider (with retry for non-stream)
   - Fallback to secondary if primary fails (before first chunk only)
   - Update passive health state
   ↓
8. Provider:
   - Call upstream API
   - Return response or stream
   ↓
9. Response flows back:
   - Usage record completed
   - Metrics recorded
   - Logs written
   - HTTP response sent
```

For detailed request flow documentation, see [request-flow.md](request-flow.md).

## Component Boundaries

### Separation of Concerns

The architecture enforces clear boundaries:

- **Handlers** parse HTTP, delegate to services, and format responses
- **Services** contain business logic and orchestrate calls
- **Providers** abstract upstream APIs behind a common interface
- **Stores** handle persistence and data access

### Dependency Direction

Dependencies flow inward:
- Handlers depend on services
- Services depend on providers and stores
- Providers and stores have no dependencies on higher layers

### Interface-Based Design

Key abstractions use interfaces:
- `ChatCompletionProvider` and `ModelProvider` for provider backends
- `Authenticator` for auth logic
- `Limiter` for rate limiting
- `MetricsRecorder` for observability

This allows:
- Easy testing with mocks
- Swapping implementations without changing callers
- Clear contracts between layers

## Configuration

The gateway is configured via:
- **Environment Variables**: `APP_*` prefixed variables for deployment-specific settings
- **Config File**: `configs/config.yaml` for structured configuration

Key configuration areas:
- Server settings (timeouts, body size limits)
- MySQL connection (DSN)
- Redis connection (address, password, db)
- Provider settings (type, base URL, API key, model, timeouts, retries)
- Gateway behavior (failure threshold, cooldown, probe interval)

For complete configuration documentation, see [../deployment/configuration.md](../deployment/configuration.md).

## Deployment

The gateway supports multiple deployment modes:

- **Local Development**: `go run ./cmd/gateway` with local MySQL/Redis
- **Docker Compose**: Full stack with MySQL, Redis, and gateway containers
- **Kubernetes**: Deployment, Service, ConfigMap, Secret, and Job manifests

For deployment guides, see:
- [Docker Compose Deployment](../deployment/docker-compose.md)
- [Kubernetes Deployment](../deployment/kubernetes.md)

## Health and Readiness

The gateway exposes health endpoints:

- **`GET /healthz`**: Always returns 200 (liveness check)
- **`GET /readyz`**: Returns 200 if at least one provider is healthy, 503 otherwise (readiness check)
- **`GET /debug/providers`**: Returns detailed provider health state (for debugging)

Readiness behavior:
- If all providers are in cooldown, `/readyz` returns 503
- Kubernetes can use this to stop routing traffic during provider outages
- Passive health tracking marks providers unhealthy after failures
- Active probing refreshes provider health periodically

## Security Considerations

- **API Key Storage**: Only SHA-256 hashes are stored, never raw keys
- **Tenant Isolation**: Each request is scoped to a single tenant via the principal
- **Request Body Limits**: Configurable max request body size prevents abuse
- **Timeouts**: All server timeouts are configured to prevent resource exhaustion
- **Rate Limiting**: RPM/TPM limits prevent quota abuse

## Performance Characteristics

- **Streaming**: SSE streams are proxied with minimal buffering for low latency
- **TTFT (Time To First Token)**: Measured and exposed via metrics
- **Fallback Constraint**: Fallback only before first chunk to avoid duplicate output
- **Connection Pooling**: HTTP clients reuse connections to upstream providers
- **Redis Caching**: Rate limit counters use Redis for fast access

For performance benchmarks, see [../verification/benchmarks/](../verification/benchmarks/).

## Observability

The gateway provides comprehensive observability:

- **Metrics**: Prometheus-style metrics on `/metrics` (request counts, latencies, failures)
- **Tracing**: Request and trace ID propagation with log-based spans plus optional OTLP/HTTP export
- **Logging**: Structured JSON logs with request/trace/tenant context
- **Dashboards**: Importable Grafana dashboard under `deployments/grafana/dashboards`

For observability details, see [observability.md](observability.md).

## Limitations

Current architectural limitations:

- **No RAG**: No retrieval-augmented generation or vector database integration
- **No Admin UI**: No web interface for tenant/key management
- **No Tenant API**: Tenant and key management is manual (via MySQL)
- **No Long-Term Analytics**: Usage data is in MySQL but not aggregated for analytics
- **No Distributed Tracing Backend**: Trace IDs are generated but not sent to a tracing system
- **No Provider-Specific Quotas**: Quotas are gateway-level, not per-provider

These are intentional scope boundaries, not bugs.

## Related Documentation

- [Request Flow](request-flow.md) - Detailed request flow through all layers
- [Provider Adapters](provider-adapters.md) - Provider abstraction and adapter design
- [Streaming Proxy](streaming-proxy.md) - SSE streaming implementation details
- [Governance](governance.md) - Multi-tenant auth, rate limiting, and budget enforcement
- [Routing and Resilience](routing-resilience.md) - Provider routing, retry, and fallback
- [Observability](observability.md) - Metrics, tracing, and logging design

## References

- Main README: [../../README.md](../../README.md)
- Existing Architecture Doc: [../architecture.md](../architecture.md)
- API Documentation: [../api/](../api/)
- Deployment Guides: [../deployment/](../deployment/)
