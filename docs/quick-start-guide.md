# Quick Start Guide for Interviewers

**Reading time: ~10 minutes**

This guide provides a rapid overview of the LLM Access Gateway project for time-constrained interviewers and recruiters. It highlights the project's scope, key technical decisions, and points to evidence of engineering rigor.

## What This Project Is

LLM Access Gateway is a **multi-tenant model access and governance gateway** written in Go. It sits between client applications and LLM providers (like OpenAI), providing:

- **Unified API**: OpenAI-compatible `/v1/chat/completions` and `/v1/models` endpoints
- **Multi-tenant governance**: API key authentication, per-tenant RPM/TPM rate limiting, and token budget enforcement
- **SSE streaming proxy**: Real-time streaming with Time-to-First-Token (TTFT) measurement
- **Resilience**: Provider routing, health tracking, automatic fallback, and retry logic
- **Observability**: Prometheus metrics, request/trace correlation, structured logging
- **Production-ready deployment**: Docker Compose and Kubernetes manifests with health checks

**Repository**: [Main README](../README.md)

## What This Project Is NOT

To understand the boundaries:

- ❌ Not a model training or fine-tuning platform
- ❌ Not an inference optimization engine
- ❌ Not a RAG or vector database system
- ❌ Not a frontend UI or agent workflow orchestrator
- ✅ **Focus**: Gateway layer for access control, governance, and resilience

This is a **focused infrastructure component** that solves the multi-tenant access and governance problem for LLM services.

## Key Technical Decisions

### 1. SSE Streaming Proxy with Fallback Constraints

**Decision**: Implement Server-Sent Events (SSE) streaming with a critical constraint: fallback to secondary providers is only allowed *before* the first chunk is sent to the client.

**Rationale**: 
- Once streaming begins, the client has started receiving data
- Switching providers mid-stream would produce inconsistent or corrupted responses
- This constraint ensures response integrity while still providing resilience for pre-stream failures

**Evidence**: 
- Implementation: [`internal/provider/router/chat.go`](../internal/provider/router/chat.go)
- Design deep-dive: [Streaming Proxy Architecture](architecture/streaming-proxy.md)
- Router waits for the first upstream event before returning the stream to the handler

### 2. Passive Health Tracking with Cooldown

**Decision**: Track provider health passively through request failures, apply a fixed cooldown window, and refresh readiness with background probes.

**Rationale**:
- Active health checks add overhead and don't reflect real request behavior
- Passive tracking uses actual request failures as health signals
- Cooldown prevents thundering herd when providers recover
- `/readyz` endpoint reflects aggregate provider health for load balancer integration

**Evidence**:
- Implementation: [`internal/provider/router/chat.go`](../internal/provider/router/chat.go)
- Debug endpoint: `GET /debug/providers` shows health state in real-time
- Metrics: `lag_provider_events_total` tracks failures and recoveries
- Current defaults: threshold `1`, cooldown `30s`, probe interval `30s`

### 3. Multi-Layer Rate Limiting with Fallback

**Decision**: Implement Redis-backed rate limiting (RPM/TPM) with automatic MySQL fallback when Redis is unavailable.

**Rationale**:
- Redis provides fast, distributed rate limiting for production
- MySQL fallback ensures the gateway remains functional during Redis outages
- Graceful degradation over hard dependency
- Token budget enforcement prevents runaway costs

**Evidence**:
- Implementation: [`internal/service/governance/redis_limiter.go`](../internal/service/governance/redis_limiter.go) and [`mysql_limiter.go`](../internal/service/governance/mysql_limiter.go)
- Tests: [`redis_limiter_test.go`](../internal/service/governance/redis_limiter_test.go)
- Metrics: `lag_governance_rejections_total` tracks rate limit enforcement

### 4. Request Tracing and Correlation

**Decision**: Generate unique request IDs and trace IDs for every request, propagate them through all layers, and expose them in responses and logs.

**Rationale**:
- Essential for debugging distributed systems
- Enables correlation between logs, metrics, and traces
- Helps diagnose failures across provider boundaries
- Critical for production troubleshooting

**Evidence**:
- Implementation: [`internal/obs/tracing/tracing.go`](../internal/obs/tracing/tracing.go)
- Every response includes `X-Request-Id` and `X-Trace-Id` headers
- Structured logs include `request_id`, `trace_id`, and `span_id` fields

### 5. Security: Hashed API Keys

**Decision**: Store only SHA-256 hashes of API keys in the database, never raw keys.

**Rationale**:
- Database compromise doesn't expose usable API keys
- Standard security practice for credential storage
- Minimal performance impact (single hash operation per request)

**Evidence**:
- Implementation: [`internal/store/mysql/auth_store.go`](../internal/store/mysql/auth_store.go)
- Bootstrap tool: [`cmd/devinit/main.go`](../cmd/devinit/main.go) shows hash generation
- Database schema: [`migrations/001_init.sql`](../migrations/001_init.sql) stores `key_hash` not `key`

## Evidence of Engineering Rigor

### Performance Benchmarking

The project includes a built-in load testing tool that measures:
- Requests per second (QPS)
- Latency percentiles (P50, P95, and max)
- Time-to-First-Token (TTFT) for streaming requests
- Success rates and error distribution

**Run it yourself**:
```bash
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 100 -concurrency 10
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 50 -concurrency 5 -stream
```

**Benchmark reports**:
- [Non-Streaming Benchmarks](verification/benchmarks/non-streaming.md)
- [Streaming Benchmarks](verification/benchmarks/streaming.md)
- [Benchmark Methodology](verification/benchmarks/methodology.md)

### Failure Drills

The project includes reproducible failure scenarios to verify resilience features:

**Provider Fallback Drill**:
```bash
./scripts/provider-fallback-drill.sh create-fail  # Force primary provider failure
./scripts/provider-fallback-drill.sh stream-fail  # Force streaming failure
```

**Smoke Test Suite**:
```bash
./scripts/gateway-smoke-check.sh  # Comprehensive API contract verification
make verify                        # Machine-verifiable acceptance tests
```

**Failure drill reports**:
- [Provider Timeout Drill](verification/failure-drills/provider-timeout.md)
- [Provider Error Drill](verification/failure-drills/provider-errors.md)
- [Quota Enforcement Drill](verification/failure-drills/quota-enforcement.md)
- [Streaming Failure Drill](verification/failure-drills/streaming-failures.md)

### Test Coverage

The codebase includes comprehensive unit tests for critical components:
- Auth service: [`internal/auth/service_test.go`](../internal/auth/service_test.go)
- Chat service: [`internal/service/chat/service_test.go`](../internal/service/chat/service_test.go)
- Governance service: [`internal/service/governance/service_test.go`](../internal/service/governance/service_test.go)
- Provider implementations: [`internal/provider/openai/chat_test.go`](../internal/provider/openai/chat_test.go), [`internal/provider/anthropic/chat_test.go`](../internal/provider/anthropic/chat_test.go), [`internal/provider/ollama/chat_test.go`](../internal/provider/ollama/chat_test.go)
- Metrics registry: [`internal/obs/metrics/registry_test.go`](../internal/obs/metrics/registry_test.go)

**Run tests**:
```bash
go test ./...
```

## Architecture Highlights

### Clean Layer Separation

```
HTTP Layer (router, handlers)
    ↓
Auth Layer (bearer token → tenant resolution)
    ↓
Governance Layer (RPM/TPM/budget checks)
    ↓
Service Layer (request validation, response shaping)
    ↓
Provider Router (health tracking, fallback)
    ↓
Provider Adapters (OpenAI-compatible, Anthropic, Ollama, mock)
```

**Key principle**: Handlers stay thin, business logic lives in services, cross-cutting concerns (auth, tracing) use middleware.

### Request Flow Example

For `POST /v1/chat/completions`:

1. **Router** applies request ID and trace middleware
2. **Auth middleware** validates bearer token, resolves tenant
3. **Governance service** checks RPM/TPM limits and token budget
4. **Chat service** validates request format
5. **Provider router** selects healthy provider, attempts request
6. **Fallback logic** retries with secondary provider if primary fails (before streaming starts)
7. **Response** includes trace headers, logs capture full correlation

### Observability Stack

- **Metrics**: Prometheus-style metrics on `/metrics` endpoint
  - Request counts and count/sum latency pairs
  - Provider failure and fallback counts
  - Governance rejection counts
  - Streaming metrics (TTFT, chunk counts)
- **Tracing**: Request and trace ID propagation through all layers, with optional OTLP/HTTP export
- **Dashboard**: Importable Grafana dashboard under `deployments/grafana/dashboards`
- **Logging**: Structured JSON logs with correlation fields

## Deployment Options

### Local Development (Docker Compose)

```bash
docker compose -f deployments/docker/docker-compose.yml up -d
```

Includes: MySQL, Redis, gateway service, and bootstrap job.

**Guide**: [Local Development](local-development.md)

### Kubernetes

```bash
kubectl apply -f deployments/k8s/namespace.yaml
kubectl apply -f deployments/k8s/configmap.yaml
kubectl apply -f deployments/k8s/secret.example.yaml
kubectl apply -f deployments/k8s/job.yaml
kubectl apply -f deployments/k8s/deployment.yaml
kubectl apply -f deployments/k8s/service.yaml
```

Includes: Namespace, ConfigMap, Secret, init Job, Deployment with health checks, and Service.

**Guide**: [Kubernetes Deployment](deployment/kubernetes.md)

## Suggested Reading Path

### For Quick Assessment (15 minutes)
1. ✅ This guide (you're here)
2. [Main README](../README.md) - Quick start and API examples
3. [Architecture Overview](architecture.md) - System design and boundaries

### For Technical Deep-Dive (1-2 hours)
1. [API Reference](api.md) - Complete endpoint documentation
2. [Architecture Deep-Dive](architecture/) - Component design decisions
3. [Benchmark Reports](verification/benchmarks/) - Performance evidence
4. [Failure Drills](verification/failure-drills/) - Resilience evidence

### For Learning and Blog Content (2-3 hours)
1. [Blog: Project Overview](blog/001-project-overview.md)
2. [Blog: SSE Streaming Implementation](blog/002-sse-streaming.md)
3. [Blog: Resilience and Failure Handling](blog/003-resilience.md)
4. [Blog: Observability](blog/004-observability.md)
5. [Blog: Multi-Tenant Governance](blog/006-multi-tenant-governance.md)

## Quick Verification

Want to see it in action? After starting the gateway locally:

```bash
# Health check
curl http://127.0.0.1:8080/healthz

# Non-streaming chat
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'

# Streaming chat
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}],"stream":true}'

# Provider health status
curl http://127.0.0.1:8080/debug/providers

# Metrics
curl http://127.0.0.1:8080/metrics
```

## Technology Stack

- **Language**: Go 1.21+
- **Database**: MySQL 8.0+ (auth, usage tracking)
- **Cache**: Redis 7.0+ (rate limiting)
- **HTTP Router**: chi (lightweight, composable)
- **Observability**: Prometheus metrics, structured logging (zap), request/trace correlation
- **Deployment**: Docker, Kubernetes

## Project Maturity

**Current Status**: Functional v1 with core features complete

✅ **Complete**:
- OpenAI-compatible API endpoints
- Multi-tenant auth and governance
- SSE streaming proxy
- Provider routing and fallback
- OpenAI-compatible, Anthropic, and Ollama provider adapters
- Observability basics
- Docker and K8s deployment
- Documentation, benchmark reports, and failure drill reports

📋 **Future Enhancements**:
- Additional hosted provider adapters beyond OpenAI-compatible / Anthropic
- Admin API for tenant management
- Enhanced metrics and dashboards
- Long-term analytics

## Questions to Explore

If you're evaluating this project, consider exploring:

1. **Streaming constraint**: Why is fallback only allowed before the first chunk? What are the trade-offs?
2. **Passive health tracking**: How does this compare to active health checks? What are the benefits and limitations?
3. **Rate limiting fallback**: What happens when Redis is down? How does the system maintain availability?
4. **Security model**: How are API keys protected? What's the threat model?
5. **Observability**: How would you debug a slow request? What correlation tools are available?

## Contact and Contribution

- **Main README**: [../README.md](../README.md)
- **Documentation Index**: [README.md](README.md)
- **Local Development Guide**: [local-development.md](local-development.md)

---

**Last Updated**: 2026-04-07
**Status**: Complete  
**Audience**: Interviewers, Recruiters, Technical Evaluators
