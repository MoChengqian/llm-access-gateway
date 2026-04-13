# Architecture Overview

This page is the shortest architecture-level summary of the current repository.
Use it to understand the system position, the request path, and the boundaries
that matter before reading the deeper architecture documents.

## System Position

LLM Access Gateway sits between client applications and upstream model
providers. It keeps the external API OpenAI-compatible while adding governance,
routing, resilience, observability, and delivery verification around that
request path.

```text
client/app
   |
   v
LLM Access Gateway
   |
   +--> provider backend A
   +--> provider backend B
   +--> provider backend N
```

At a high level, the gateway is responsible for:

- API key authentication and tenant resolution
- RPM, TPM, and token-budget governance
- provider routing and fallback
- streaming proxy behavior
- request and trace correlation
- local and deployment-facing verification

It is not responsible for:

- model training
- inference acceleration
- RAG or vector retrieval
- frontend UI
- agent workflow orchestration

## The Main Request Path

For `POST /v1/chat/completions`, the request path is:

```text
router
  -> auth
  -> governance
  -> chat service
  -> provider router
  -> provider adapter
  -> upstream provider
```

The practical meaning of that order is:

1. the router establishes request IDs, trace IDs, metrics, and logging context
2. auth resolves `Authorization: Bearer <key>` into a tenant-scoped principal
3. governance checks quota and budget before provider work starts
4. the chat service validates and normalizes gateway-level behavior
5. the provider router chooses a backend and enforces fallback rules
6. the provider adapter translates the request into upstream-specific form

For the detailed sequence, read [Request Flow](request-flow.md).

## The Architectural Boundaries That Matter

### 1. Handlers Stay Thin

HTTP handlers are intentionally small. They parse HTTP concerns and delegate
business behavior to services.

Why this matters:

- request behavior remains testable without HTTP coupling
- auth, governance, and provider logic do not get buried in handlers
- new endpoints can reuse the same service and observability contracts

### 2. Governance Happens Before Provider Invocation

Authentication and quota checks are part of the request path, not optional
wrappers around it.

The gateway enforces:

- MySQL-backed API key and tenant resolution
- RPM limiting
- TPM limiting
- token-budget checks
- request usage recording

This keeps the system grounded in multi-tenant access governance rather than
just protocol forwarding.

### 3. Backend Definitions And Routing Policy Are Separate

Configured backends still define credentials and provider adapter settings, but
backend choice can be driven by persisted MySQL `route_rules`.

That split is important:

- config owns runtime wiring and secrets
- MySQL `route_rules` can own request-routing policy
- routing changes can be managed without rewriting handler logic

When enabled `route_rules` exist, they are authoritative for candidate
selection. When they do not, the router falls back to config-local model and
priority ranking.

For the detailed behavior, read [Routing & Resilience](routing-resilience.md).

### 4. Streaming Fallback Stops At The First Chunk Boundary

Streaming fallback is allowed only before the first successful upstream chunk
has been accepted.

After that point:

- the gateway can forward interruption
- the backend can still be marked unhealthy
- the stream can end without a false `[DONE]`
- the gateway cannot safely switch to another provider

This is one of the most important design boundaries in the project because it
turns “resilience” from a vague promise into a testable contract.

### 5. Observability Is Part Of The Request Contract

Every request gets:

- `X-Request-Id`
- `X-Trace-Id`
- structured logs
- Prometheus-style metrics

Optional OTLP/HTTP export uses the same request and span lifecycle rather than
a separate tracing model. That keeps local debugging and external trace export
aligned.

For deeper detail, read [Observability Design](observability.md).

### 6. Repository Verification And Environment Rollout Are Different Gates

The repository now draws a hard boundary between:

- repository-owned verification
- environment-owned rollout readiness

Examples:

- `make stage7-static` and `make k8s-production-local-check` are repository
  gates
- `make k8s-production-server-dry-run` is an environment gate that only applies
  when a real cluster exists

This matters because the repository should not fake production readiness it
does not actually own.

## Main Runtime Dependencies

The core runtime dependencies are:

- MySQL for tenants, API keys, route rules, and usage records
- Redis for fast limiter counters, with MySQL fallback behavior
- upstream model providers through OpenAI-compatible, Anthropic, Ollama, or
  mock adapters

In other words:

- MySQL is the source of truth for governance and persisted policy
- Redis improves limiter performance but is not the only path
- provider adapters isolate upstream API differences from the gateway core

## Delivery Shape

The repository ships delivery assets at three levels:

- local shell and Docker Compose paths
- baseline Kubernetes manifests
- production and optional HPA overlays with verification entrypoints

That delivery layer is part of the architecture story because the repository is
trying to prove not only request-path logic, but also runnable and reviewable
delivery behavior.

## Read Next

Choose one deeper document based on what you care about:

- request sequence:
  [Request Flow](request-flow.md)
- provider routing, health, and fallback:
  [Routing & Resilience](routing-resilience.md)
- auth, quota, and usage behavior:
  [Governance](governance.md)
- streaming behavior:
  [Streaming Proxy](streaming-proxy.md)
- metrics, tracing, and logs:
  [Observability Design](observability.md)
