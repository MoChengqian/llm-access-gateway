# Execution Roadmap

This document is the canonical execution roadmap for engineers working on `LLM Access Gateway`.

It translates the project goal into staged delivery work so an execution engineer can answer four questions quickly:

1. what this project is trying to prove
2. what is inside v1 and what is outside v1
3. which stage a task belongs to
4. what must be delivered before the stage can be considered complete

## Who Should Use This Document

Use this roadmap when you are:

- planning the next implementation slice
- deciding whether a task belongs in v1
- checking what "done" means for a stage
- preparing documentation, verification, or delivery artifacts

Do not use this document as a marketing page. It is an execution reference.

## Project Goal

### Project Name

**LLM Access Gateway: Multi-Tenant Model Access and Governance Gateway**

### What This Project Is

This is a gateway system for multi-tenant LLM service access. Its focus is not model training, inference optimization, or AI application assembly. Its focus is the request path in front of model services:

- unified access
- SSE streaming proxy
- multi-tenant authentication
- quota governance
- routing and fallback
- observability
- containerized delivery
- load testing and failure drills

### What This Project Must Prove

By the end of v1, the project should prove:

- LLM traffic can be abstracted as a gateway and governance problem
- gateway, routing, and resilience patterns can be migrated into the model access layer
- a bounded engineering system can be built from `0 -> 1` with clean interfaces
- the result is reviewable, verifiable, and reproducible as engineering evidence

## v1 Boundary

### In Scope

v1 must include:

- OpenAI-compatible external API shape
- `POST /v1/chat/completions`
- `GET /v1/models`
- provider adapters
- non-streaming request path
- SSE streaming proxy
- API key authentication
- tenant resolution
- MySQL persistence for tenant, API key, routing/governance data, and usage
- Redis-backed RPM limiting
- usage recording
- health checks
- primary/secondary fallback
- Prometheus-style metrics
- baseline structured logging
- Docker Compose local delivery
- baseline load testing
- baseline failure drills

### Out of Scope

v1 does not include:

- RAG
- vector databases
- agent workflow
- frontend chat UI
- inference kernel optimization
- KV cache
- fine-tuning
- operator fusion
- deep Triton, vLLM, or SGLang optimization work

## Execution Principles

All execution work should follow these principles:

1. Preserve the system boundary. This is a gateway project, not an AI application project.
2. Keep handlers thin and push business logic into services.
3. Prefer evidence-producing work over feature sprawl.
4. Every stage should produce something runnable or reviewable.
5. When implementation and docs disagree, fix the contract before adding scope.
6. Treat reproducibility as part of the feature, not post-processing.

## Current Baseline

### Repository Baseline As Of 2026-04-10

The current repository is already beyond the early scaffold stages.

Rough status:

- Stage 0: complete for the v1 repository contract
- Stage 1: complete
- Stage 2: complete
- Stage 3: complete
- Stage 4: complete
- Stage 5: complete in baseline form, with persisted `route_rules` now driving backend selection
- Stage 6: complete for v1; lightweight metrics remain, and trace export/dashboard assets are now present
- Stage 7: complete for v1; delivery, load, drill, and nightly assets now share a standard verification contract

Current known gaps that still matter operationally:

- production metrics backends, tracing storage, and Grafana provisioning remain environment-owned
- external load tools such as `k6` remain intentionally deferred unless they add coverage beyond `cmd/loadtest`
- long-duration soak and resource-trend evidence are still outside the committed v1 verification contract

When in doubt, treat this roadmap as the target contract and the current codebase as the moving baseline.

## Phase Plan

## Stage 0: Project Scaffold And Contract Stabilization

### Objective

Lock the repository shape, baseline docs, local startup path, and schema contract so a new engineer can run and understand the project quickly.

### Target Scope

- repository structure
- basic startup flow
- core docs
- migration contract
- Make targets
- environment examples

### Concrete Tasks

1. Keep the top-level repository layout stable.
2. Maintain a runnable `cmd/gateway`.
3. Keep `README.md` accurate for local startup.
4. Keep `docs/prd-v1.md` aligned with the real v1 scope.
5. Keep `docs/architecture.md` and the architecture sub-docs aligned with implementation.
6. Make sure `migrations/001_init.sql` matches the database shape required by the runtime path.
7. Keep `Makefile` targets usable.
8. Keep `.env.example` representative of the current runtime surface.

### Acceptance Criteria

- a new engineer can get the repo running in about 10 minutes
- `go test ./...` passes
- `/healthz` and `/readyz` work
- non-stream and stream mock paths both work locally
- docs do not describe an older system than the codebase actually implements

### Deliverables

- `README.md`
- `docs/prd-v1.md`
- `docs/architecture.md`
- initial migration SQL
- `Makefile`
- `.env.example`

### Current Delta In This Repo

Stage 0 is complete for the current repository contract:

- `docs/prd-v1.md` reflects the real gateway scope rather than the early scaffold
- `migrations/001_init.sql` includes the runtime tables used by auth, usage, attempt usage, and routing
- `.env.example` covers the current server, persistence, observability, gateway, and provider runtime surface

Execution engineers should now preserve that contract instead of reopening it
without evidence of new drift.

## Stage 1: Minimum Access Layer Loop

### Objective

Build the minimal "unified API -> service -> provider" request path.

### Target Scope

- `POST /v1/chat/completions`
- `GET /v1/models`
- provider abstraction
- request and response normalization

### Concrete Tasks

1. Define unified completion request structs.
2. Define unified completion response structs.
3. Define streaming chunk structs.
4. Extract provider interfaces.
5. keep a mock provider available for local verification.
6. implement `/v1/models`.
7. cover service and provider behavior with unit tests.

### Acceptance Criteria

- non-stream requests return stable unified JSON
- stream requests return unified chunks
- mock provider can be swapped without changing handlers
- `/v1/models` works through the service layer

### Deliverables

- provider abstraction
- mock provider
- unified request and response protocol
- `/v1/models`

### Current Delta In This Repo

This stage is already complete in the current repository. New work should not re-open the interface boundary unless there is a strong reason.

## Stage 2: SSE Streaming Proxy Hardening

### Objective

Make the streaming layer one of the system's strongest and most defensible pieces.

### Target Scope

- OpenAI-style chunk shape
- streaming lifecycle semantics
- TTFT measurement
- disconnect and cancel propagation
- streaming verification

### Concrete Tasks

1. Keep stream chunks in OpenAI-style `delta` shape.
2. Keep single-choice `index` stable.
3. Record time to first token.
4. Stop upstream work on client disconnect or context cancellation.
5. Record stream-specific access and metrics fields.
6. Maintain SSE integration coverage.
7. Maintain at least one longer-running stream verification path.

### Acceptance Criteria

- `Content-Type: text/event-stream`
- multiple `data:` chunks
- terminal `data: [DONE]`
- no obvious goroutine leakage after client cancel
- TTFT can be observed and reported

### Deliverables

- SSE implementation
- streaming design documentation
- SSE tests
- curl demonstration output

### Current Delta In This Repo

This stage is already implemented. Future work here should focus on regression prevention, not redesign.

## Stage 3: Multi-Tenant Authentication Loop

### Objective

Move the project from a gateway demo to a governance-oriented gateway.

### Target Scope

- bearer token parsing
- API key verification
- tenant resolution
- context-injected principal
- auth error mapping

### Concrete Tasks

1. Parse `Authorization: Bearer <key>` consistently.
2. Reject missing keys with `401`.
3. Reject malformed keys with `401`.
4. Reject invalid keys with `401`.
5. Reject disabled or expired identities with the configured error mapping.
6. Inject `tenant_id` and `api_key_id` into context.
7. Read identity from context in handlers and services.
8. Include tenant and API key identity in access logs.
9. Provide a repeatable local seed/init path for development keys.

### Acceptance Criteria

- missing key is rejected
- wrong key is rejected
- disabled key is rejected
- valid key reaches the business path
- both stream and non-stream routes are protected

### Deliverables

- auth service
- auth middleware
- tenant context
- auth tests
- local seed/init command

### Current Delta In This Repo

This stage is already complete. Any future changes should preserve the "hashed key only" contract.

## Stage 4: Quota And Usage Governance

### Objective

Add the minimum governance layer that makes cost and tenancy real.

### Target Scope

- tenant RPM limiting
- token-based limits
- request usage recording
- attempt-level usage recording
- budget enforcement

### Concrete Tasks

1. Enforce RPM by tenant with Redis as the fast path.
2. Keep a MySQL fallback path when Redis is unavailable.
3. Record request usage on completion.
4. Record attempt usage for retries and fallback attempts.
5. Store provider, model, stream flag, latency-related fields, and status.
6. Enforce token budget before admission and before extra attempts when needed.
7. Expose usage query APIs and summaries for tenant inspection.

### Acceptance Criteria

- a tenant that exceeds RPM is rejected
- usage records land in MySQL
- budget exhaustion can reject requests
- both stream and non-stream requests are represented in usage data
- retry and fallback work is visible in attempt-level usage records

### Deliverables

- Redis limiter design
- MySQL fallback limiter
- request and attempt usage persistence
- usage API
- governance tests

### Current Delta In This Repo

This stage is already mostly complete. The main remaining requirement is contract cleanup so migrations and docs match the runtime tables and fields.

## Stage 5: Routing, Health Check, And Fallback

### Objective

Turn the gateway from "can accept requests" into "can survive upstream failure."

### Target Scope

- backend selection
- health state
- fallback
- active probing
- stream fallback boundary

### Concrete Tasks

1. Support ordered backend registration and provider registry behavior.
2. Support model-aware backend preference.
3. Support primary and fallback backends.
4. Maintain provider health state and cooldown.
5. Run periodic health probes.
6. Allow retry or fallback for non-stream requests.
7. Allow fallback for stream requests only before the first chunk.
8. Record fallback events in logs and metrics.
9. Expose health state through `/readyz` and `/debug/providers`.

### Acceptance Criteria

- primary backend failure can shift traffic to fallback backend
- metrics show fallback counts
- backend health status is queryable
- streaming never falls back after the first accepted chunk

### Deliverables

- provider router
- health and cooldown design
- failure drill records
- fallback demo and docs

### Current Delta In This Repo

This stage is now present with persisted routing policy and baseline operator workflow:

- provider endpoint definitions still come from config
- enabled rows in `route_rules` decide which configured backends participate
- route rules are loaded at process startup and exposed through `/debug/providers`
- `cmd/routerulectl` provides list, replace, and sync-from-config management for route policy

The next evolution, if required, would be operational rather than foundational:

- add hot reload behavior for route policy changes without restart

## Stage 6: Observability Completion

### Objective

Make the system not only maintainable, but provable.

### Target Scope

- request correlation
- structured logs
- gateway metrics
- provider metrics
- stream metrics
- tracing
- dashboards or exporter integration

### Concrete Tasks

1. Keep request IDs on every request.
2. Keep trace IDs visible to clients and logs.
3. Standardize access log fields across success and failure paths.
4. Publish gateway and provider metrics on `/metrics`.
5. Record governance rejection reasons.
6. Record stream TTFT and chunk counters.
7. Keep trace spans across request, service, and provider layers.
8. Add OTLP or equivalent external export capability when the repo is ready.
9. Add dashboard assets once the exported metrics contract is stable.

### Acceptance Criteria

- request volume and failure counts can be inspected
- request latency is observable
- provider failure, fallback, and readiness are observable
- a request can be traced by `request_id` or `trace_id`
- the observability story is reproducible, not anecdotal

### Deliverables

- logging conventions
- metrics registry
- tracing utilities
- `/metrics`
- observability design docs
- dashboard or exporter assets when introduced

### Current Delta In This Repo

Stage 6 is complete for the v1 repository contract:

- structured logs are present
- trace correlation is present
- `/metrics` is present
- trace spans can optionally be exported over OTLP/HTTP through
  `APP_OBSERVABILITY_OTLP_TRACES_ENDPOINT`
- OTLP export can be verified locally through `cmd/otlpstub` and
  `scripts/otlp-export-check.sh`
- a repository-owned local demo stack now boots OpenTelemetry Collector,
  Prometheus, and Grafana from `deployments/observability/`, with
  `scripts/observability-demo-check.sh` asserting metrics scrape, accepted spans,
  and dashboard provisioning
- `scripts/observability-demo-prepull.sh` and
  `scripts/observability-demo-verify.sh` standardize image warming and the
  end-to-end local runtime verification loop
- the Grafana dashboard asset is committed at
  `deployments/grafana/dashboards/llm-access-gateway.json`

Remaining future hardening is intentionally outside Stage 6:

- push-based metrics export
- histogram buckets for percentile latency panels
- production Grafana/Prometheus/Tempo provisioning owned by an environment repo
- persisted trace storage and long-term metrics retention

## Stage 7: Delivery, Load Testing, And Failure Drills

### Objective

Push the project from "development complete" toward "operationally demonstrable system."

### Target Scope

- Docker delivery
- Kubernetes delivery
- local bootstrap
- benchmark tooling
- failure drill tooling
- repeatable verification artifacts

### Concrete Tasks

1. Keep the Docker build path healthy.
2. Keep Docker Compose as the primary local stack.
3. Keep Kubernetes manifests usable as baseline delivery assets.
4. Maintain a repeatable bootstrap command for schema and seed data.
5. Maintain load test tooling for both stream and non-stream paths.
6. Maintain failure drill scripts for provider timeout, provider error, quota rejection, and stream interruption scenarios.
7. Keep benchmark methodology and result documents reproducible.
8. Add CI or nightly verification where it meaningfully protects the contract.
9. Add external load assets such as `k6` only if they provide incremental verification value beyond the built-in tools.

### Acceptance Criteria

- Docker Compose can bring up the local stack
- baseline Kubernetes manifests are runnable after environment wiring
- load results are reproducible
- failure scenarios are documented and repeatable
- the system does not show obvious crash, leak, or silent-failure behavior under those drills

### Deliverables

- `deployments/docker/docker-compose.yml`
- `deployments/k8s/*`
- load tooling
- benchmark docs
- failure drill docs
- nightly or CI verification assets

### Current Delta In This Repo

Stage 7 is complete for the v1 repository contract:

- Docker Compose remains the canonical local stack
- Kubernetes manifests remain the baseline delivery assets
- `cmd/devinit` is the repeatable schema and seed-data bootstrap command
- `cmd/loadtest` is the canonical load tool for both stream and non-stream paths
- failure drills are documented and mapped to scripts or nightly checks
- `.github/workflows/runtime-ci.yml` and `.github/workflows/nightly-verification.yml`
  protect the runtime contract
- `scripts/stage7-verify.sh` standardizes static and runtime verification

Remaining future hardening is intentionally outside Stage 7:

- add `k6` only if it proves materially different coverage from `cmd/loadtest`
- add production environment overlays for ingress, secret management, and
  managed observability stacks
- add long-duration resource trend collection when the project needs sustained
  soak evidence

## Recommended Execution Order

When choosing the next implementation slice, follow this order:

1. fix contract drift first
2. finish database-driven routing and policy control
3. complete observability standardization
4. standardize delivery and verification assets
5. only then expand scope

Expanded sequence:

1. scaffold and docs contract
2. provider abstraction
3. non-stream path
4. SSE path
5. MySQL-backed auth
6. auth middleware and context
7. Redis RPM
8. usage and attempt usage records
9. route rules and health policy
10. fallback hardening
11. metrics, logging, tracing standardization
12. Docker Compose
13. Kubernetes
14. load and failure verification
15. final packaging for README, blogs, and interview evidence

## Priority Guidance

### Do Now

- keep docs, migration SQL, and runtime config aligned as new changes land
- keep CI and nightly reporting preserving the primary failing signal
- maintain the standardized Stage 7 verification contract

### Do Next

- externalize observability where useful
- keep benchmark and drill automation healthy
- add production overlays or long-duration soak evidence only when the project needs them

### Do Not Do Now

- frontend chat pages
- RAG or vector work
- agent workflow
- provider sprawl without governance value
- scope that weakens the gateway and governance positioning

## How To Use This Roadmap During Execution

When picking up a task:

1. identify the stage it belongs to
2. verify whether the task reduces a current delta or expands scope
3. define the runnable or reviewable deliverable before coding
4. define the acceptance check before coding
5. update docs and verification evidence as part of the same slice

If a task does not clearly support gateway access, governance, resilience, observability, or delivery, it probably does not belong in v1.
