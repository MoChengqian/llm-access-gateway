# LLM Access Gateway Documentation

LLM Access Gateway is a Go-based multi-tenant AI gateway for model access
governance: it keeps the external API OpenAI-compatible while making routing
policy, quota enforcement, fallback behavior, observability, and delivery
verification explicit and reviewable.

This documentation is organized around decisions, runnable paths, and evidence.
If you are new to the repository, do not read it folder by folder. Start with
the path that matches your goal.

## Start With One Path

### 1. Understand The Project Quickly

Read these in order:

1. [Quick Start Guide](quick-start-guide.md)
2. [Architecture Overview](architecture/overview.md)
3. [Execution Roadmap](execution-roadmap.md)

Use this path when you want the shortest explanation of:

- what the project is
- what it is not
- what v1 is trying to prove
- which stages are already complete

### 2. Run The Gateway Locally

Read these in order:

1. [Local Development](local-development.md)
2. [API Reference](api.md)
3. [Deployment: Docker Compose](deployment/docker-compose.md)

Use this path when you want to:

- start MySQL, Redis, `devinit`, and the gateway
- call `/v1/chat/completions`, `/v1/models`, and `/v1/usage`
- inspect `/healthz`, `/readyz`, `/debug/providers`, and `/metrics`

### 3. Verify The Engineering Evidence

Read these in order:

1. [Verification Index](verification/README.md)
2. [Stage 7 Delivery Contract](verification/stage7-delivery-contract.md)
3. [Stage 7 Production Readiness](verification/stage7-production-readiness.md)

Use this path when you want to see:

- which commands are the real verification entrypoints
- what Stage 5, Stage 6, and Stage 7 currently prove
- which checks are local repository checks versus environment-owned rollout
  checks

## Core Reading Paths

### Product Boundary And System Shape

- [Execution Roadmap](execution-roadmap.md)
- [Architecture Overview](architecture/overview.md)
- [API Reference](api.md)

These documents define the system boundary: multi-tenant model access,
governance, routing, observability, and delivery verification. They are the
best starting point for understanding what belongs in this repository and what
does not.

### Routing, Governance, And Fallback

- [Routing & Resilience](architecture/routing-resilience.md)
- [Authentication](api/authentication.md)
- [API Endpoints](api/endpoints.md)
- [Provider Error Drill](verification/failure-drills/provider-errors.md)
- [Streaming Failure Drill](verification/failure-drills/streaming-failures.md)

Use this path for the Stage 5 core:

- MySQL-backed `route_rules`
- quota and usage governance
- health-aware deterministic fallback
- streaming first-chunk fallback boundary

### Observability

- [Observability Design](architecture/observability.md)
- [OTLP Export Verification](verification/otlp-export.md)
- [Observability Demo Runtime](verification/observability-demo-runtime.md)

Use this path for the Stage 6 core:

- `X-Request-Id` and `X-Trace-Id`
- structured logs and trace/span correlation
- `/metrics`
- optional OTLP export and local Grafana/Prometheus evidence

### Delivery, Load, And Deployment

- [Stage 7 Delivery Contract](verification/stage7-delivery-contract.md)
- [Stage 7 Production Readiness](verification/stage7-production-readiness.md)
- [Deployment: Docker Compose](deployment/docker-compose.md)
- [Deployment: Kubernetes](deployment/kubernetes.md)
- [Production Considerations](deployment/production-considerations.md)
- [Benchmark Methodology](verification/benchmarks/methodology.md)

Use this path for the Stage 7 core:

- local delivery and smoke verification
- built-in load tooling and nightly regression
- Kubernetes render and preflight checks
- the boundary between repository completion and environment rollout readiness

## If You Only Read Three Documents

- [Quick Start Guide](quick-start-guide.md)
- [Architecture Overview](architecture/overview.md)
- [Verification Index](verification/README.md)

That set gives a new engineer, reviewer, or interviewer the fastest route to
the project position, internal shape, and proof trail.

## Blog And Long-Form Evidence

If you want the narrative layer behind the implementation:

- [001: Project Overview](blog/001-project-overview.md)
- [003: Resilience & Failure Handling](blog/003-resilience.md)
- [004: Observability Implementation](blog/004-observability.md)
- [006: Multi-Tenant Governance](blog/006-multi-tenant-governance.md)

These articles are strongest when read after the architecture and verification
entrypoints above. They are supporting evidence, not the main navigation path.
