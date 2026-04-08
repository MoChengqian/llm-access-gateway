# LLM Access Gateway Documentation

Welcome to the LLM Access Gateway documentation. This index organizes all documentation by audience to help you find what you need quickly.

## Quick Navigation by Audience

### 🔧 For Engineers
**Goal:** Understand, deploy, or contribute to the gateway

**Suggested Reading Path:**
1. [Quick Start Guide](quick-start-guide.md) ⭐ **Start here** - 10-minute overview
2. [Local Development](local-development.md) - Get the gateway running locally
3. [API Documentation](api.md) - Complete API reference
4. [Architecture Overview](architecture.md) - System design and components
5. [Deployment Guides](#deployment-documentation) - Docker Compose and Kubernetes

**Key Documents:**
- [API Reference](api.md) - All HTTP endpoints with curl examples ✅
- [Architecture](architecture.md) - System design and request flow ✅
- [Local Development](local-development.md) - Step-by-step local setup ✅
- [Deployment: Docker Compose](deployment/docker-compose.md) - Container deployment ✅
- [Deployment: Kubernetes](deployment/kubernetes.md) - K8s deployment ✅
- [Configuration Reference](deployment/configuration.md) - Environment variables and config ✅

### 🎯 For Interviewers & Recruiters
**Goal:** Assess technical depth and engineering quality in under 10 minutes

**Suggested Reading Path:**
1. [Quick Start Guide](quick-start-guide.md) ⭐ **Start here** - Project scope and key decisions
2. [Architecture Overview](architecture.md) - Design decisions and trade-offs
3. [Performance Benchmarks](#performance-verification) - Quantitative evidence
4. [Failure Drills](#resilience-verification) - Resilience testing results

**Key Documents:**
- [Quick Start Guide](quick-start-guide.md) - 10-minute project overview ✅
- [Architecture](architecture.md) - System design and boundaries ✅
- [Benchmark Reports](verification/benchmarks/) - Performance metrics ✅
- [Failure Drill Reports](verification/failure-drills/) - Resilience evidence ✅

### 📚 For Learners & Blog Readers
**Goal:** Learn from engineering decisions and implementation details

**Suggested Reading Path:**
1. [Project Overview Article](blog/001-project-overview.md) - Why this gateway exists
2. [SSE Streaming Implementation](blog/002-sse-streaming.md) - Building streaming proxies
3. [Resilience & Failure Handling](blog/003-resilience.md) - Retry, fallback, health checks
4. [Observability Implementation](blog/004-observability.md) - Metrics, tracing, logs
5. [Performance Benchmarking](blog/005-performance.md) - How to measure system performance
6. [Multi-Tenant Governance](blog/006-multi-tenant-governance.md) - Tenant isolation and quotas

**Key Documents:**
- [Blog Articles](blog/) - Technical deep-dives and lessons learned ✅

---

## Documentation by Category

### API Documentation
Complete reference for all HTTP endpoints with request/response examples.

- [API Reference](api.md) - All endpoints, auth, streaming format ✅
- [API Endpoints](api/endpoints.md) - Detailed endpoint specifications ✅
- [Authentication](api/authentication.md) - Auth requirements and flows ✅
- [SSE Streaming](api/streaming.md) - Streaming format details ✅

### Architecture Documentation
Deep technical explanation of system design, components, and decisions.

- [Architecture Overview](architecture.md) - System layers and boundaries ✅
- [Request Flow](architecture/request-flow.md) - Request path through layers ✅
- [Provider Adapters](architecture/provider-adapters.md) - Provider abstraction design ✅
- [Streaming Proxy](architecture/streaming-proxy.md) - SSE proxy implementation ✅
- [Governance Model](architecture/governance.md) - Auth, tenant, quota enforcement ✅
- [Routing & Resilience](architecture/routing-resilience.md) - Routing, retry, fallback ✅
- [Observability Design](architecture/observability.md) - Metrics, tracing, logs ✅

### Deployment Documentation
Practical guides for deploying the gateway in different environments.

- [Local Development](local-development.md) - Local setup with Docker Compose ✅
- [Docker Compose Deployment](deployment/docker-compose.md) - Container deployment guide ✅
- [Kubernetes Deployment](deployment/kubernetes.md) - K8s deployment guide ✅
- [Configuration Reference](deployment/configuration.md) - All config options ✅
- [Production Considerations](deployment/production-considerations.md) - Production deployment advice ✅

### Performance Verification
Benchmark reports with quantitative performance metrics.

- [Non-Streaming Benchmarks](verification/benchmarks/non-streaming.md) - QPS and latency ✅
- [Streaming Benchmarks](verification/benchmarks/streaming.md) - TTFT metrics ✅
- [Benchmark Methodology](verification/benchmarks/methodology.md) - Test approach and environment ✅

### Resilience Verification
Failure drill reports demonstrating system behavior under failure conditions.

- [Provider Timeout Drill](verification/failure-drills/provider-timeout.md) - Timeout and fallback ✅
- [Provider Error Drill](verification/failure-drills/provider-errors.md) - 5xx error handling ✅
- [Quota Enforcement Drill](verification/failure-drills/quota-enforcement.md) - Rate limiting ✅
- [Streaming Failure Drill](verification/failure-drills/streaming-failures.md) - Stream failure scenarios ✅

### Blog Articles
Educational articles showcasing engineering decisions and implementation evidence.

- [001: Project Overview](blog/001-project-overview.md) - Gateway positioning and scope ✅
- [002: SSE Streaming Implementation](blog/002-sse-streaming.md) - Building streaming proxies ✅
- [003: Resilience & Failure Handling](blog/003-resilience.md) - Retry, fallback, health checks ✅
- [004: Observability Implementation](blog/004-observability.md) - Metrics, tracing, logs ✅
- [005: Performance Benchmarking](blog/005-performance.md) - Measuring system performance ✅
- [006: Multi-Tenant Governance](blog/006-multi-tenant-governance.md) - Tenant isolation and quotas ✅

### Maintenance & Guidelines
Templates and guidelines for creating consistent documentation.

- [Documentation Templates](maintenance/templates/) - Reusable document structures ✅
- [Writing Guidelines](maintenance/guidelines/) - Style and formatting standards ✅

---

## Document Status Legend

- ✅ **Complete** - Document is finished and up-to-date
- 🚧 **Draft** - Document is in progress or incomplete
- ⚠️ **Outdated** - Document needs updating to match current code

---

## Additional Resources

### Repository Links
- [Main README](../README.md) - Project overview and quick start
- [PRD v1](prd-v1.md) - Original product requirements

### Quick Reference
- **Local Dev Key:** `lag-local-dev-key` (after running `go run ./cmd/devinit`)
- **Base URL:** `http://127.0.0.1:8080`
- **Auth Header:** `Authorization: Bearer <api-key>`
- **Health Check:** `GET /healthz`
- **Readiness Check:** `GET /readyz`
- **Metrics:** `GET /metrics`

### Common Commands
```bash
# Start local environment
docker compose -f deployments/docker/docker-compose.yml up -d
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'
go run ./cmd/devinit
go run ./cmd/gateway

# Run tests
go test ./...
make test

# Run load tests
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 20 -concurrency 4
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 10 -concurrency 2 -stream

# Verify deployment
make verify
./scripts/gateway-smoke-check.sh
```

---

## Contributing to Documentation

When adding new documentation:

1. Use the appropriate [template](maintenance/templates/) for your document type
2. Follow the [writing guidelines](maintenance/guidelines/)
3. Update this index with a link to your new document
4. Mark the document status appropriately (✅ 🚧 ⚠️)
5. Add bidirectional links to related documents

For questions or suggestions, please open an issue in the repository.

---

**Last Updated:** 2026-04-07
**Documentation Version:** 1.0
