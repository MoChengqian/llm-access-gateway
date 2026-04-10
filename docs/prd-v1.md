# PRD V1

## Product Goal

Ship a reviewable and runnable LLM access gateway that proves model access can be
treated as a gateway and governance problem rather than an application-layer
chat UI problem.

The v1 system must provide a clean, evidence-producing request path in front of
upstream model providers:

- unified OpenAI-compatible API surface
- streaming and non-streaming chat completions
- multi-tenant authentication and quota enforcement
- provider routing, retry, health tracking, and fallback
- usage persistence and operator-facing debugging signals

## Target User

This repo is aimed at engineers who want to evaluate or demonstrate:

- how to build a gateway in front of LLM providers
- how to model auth, tenancy, quotas, and usage accounting
- how to handle SSE proxying, upstream retries, and fallback safely
- how to package the result as a reproducible engineering artifact

It is not a general AI product, agent platform, or frontend application.

## In Scope

### API surface

- `POST /v1/chat/completions`
- `GET /v1/models`
- `GET /v1/usage`
- `GET /healthz`
- `GET /readyz`
- `GET /debug/providers`
- `GET /metrics`

### Request path and provider support

- OpenAI-compatible request and response normalization
- non-streaming JSON completions
- SSE streaming proxy with terminal `[DONE]`
- provider adapter abstraction
- mock, OpenAI-compatible, Anthropic, and Ollama provider adapters
- provider timeout and pre-stream retry behavior for hosted upstreams
- model-aware priority routing and primary/secondary fallback
- active provider probing through the models endpoint

### Governance and persistence

- API key authentication
- tenant resolution from API key
- MySQL-backed tenant, API key, request usage, and request attempt usage data
- Redis-backed RPM limiting with MySQL fallback
- token-per-minute and total token budget enforcement
- seeded local development tenant and API key through `cmd/devinit`

### Operability and delivery

- structured zap logging with request and trace identifiers
- in-process Prometheus-style metrics on `/metrics`
- provider health visibility on `/debug/providers`
- Docker Compose local delivery path
- baseline Kubernetes manifests
- built-in load test command
- documented failure drills and benchmark reports

## Explicitly Out of Scope

The current v1 repository does not include:

- OTLP exporters, external tracing backends, or Grafana dashboards
- weighted traffic splitting or advanced policy routing
- frontend chat UI
- agent workflows or RAG
- vector databases
- inference engine optimization such as vLLM, Triton, or KV cache tuning

## Current Product Contract

This document describes the current repository contract as of `2026-04-10`, not
the earlier mock-only scaffold.

Important current constraints:

- provider endpoint credentials and transport settings still come from process config
- backend selection policy is now persisted in MySQL through `route_rules`
- observability is intentionally lightweight: request IDs, trace IDs, logs, and
  in-process metrics are present, but external telemetry backends are not
- the repo is already beyond pure mock behavior, so documentation must describe
  real auth, governance, persistence, and fallback behavior

## Acceptance Criteria

The v1 repository is considered acceptable when all of the following are true:

- `go run ./cmd/gateway` starts with a valid `APP_MYSQL_DSN`
- `go run ./cmd/devinit` creates the development tenant and API key
- `/healthz` returns `200`
- `/readyz` returns `200` when at least one backend is healthy
- `/v1/chat/completions` supports both `stream=false` and `stream=true`
- `/v1/models` returns the aggregated model view from healthy providers
- `/v1/usage` returns tenant-scoped usage summary and recent records
- invalid or missing API keys are rejected with `401`
- quota violations return `429` or `403` with stable JSON errors
- provider failures before first output can trigger fallback
- usage writes persist both request-level and attempt-level records
- `go test ./...` passes
- Docker Compose local startup remains documented and runnable

## Success Evidence

The repo should provide enough evidence for an engineer or interviewer to verify
the system without reverse-engineering the codebase:

- runnable local startup path
- migration contract aligned with runtime schema
- representative `.env.example`
- architecture and deployment docs that describe the real system
- benchmark and failure-drill artifacts that demonstrate behavior
