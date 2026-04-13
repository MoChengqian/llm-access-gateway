# LLM Access Gateway

LLM Access Gateway is a Go-based multi-tenant model access and governance
gateway. It keeps the external API OpenAI-compatible while making routing
policy, quota enforcement, fallback behavior, observability, and delivery
verification explicit and reviewable.

This repository is not a chatbot app, a RAG stack, or a model-serving kernel.
Its focus is the control path in front of model providers: who can call, where
traffic goes, how failures fall back, how requests are observed, and how the
system is verified.

## What This Repository Proves

- gateway access can be treated as a bounded infrastructure problem
- routing policy can move into MySQL `route_rules` instead of living only in
  process config
- the request path can combine MySQL auth, Redis-backed limit checks, MySQL
  usage records, and deterministic fallback without collapsing into framework
  sprawl
- SSE streaming fallback has a real first-chunk boundary that is implemented,
  tested, and documented
- observability and delivery can be verified locally without pretending the
  repository owns every production dependency

## Core Capabilities

### Stage 5: Governance, Routing, and Fallback

- OpenAI-compatible `POST /v1/chat/completions` and `GET /v1/models`
- MySQL-backed tenant and API key authentication
- Redis-backed RPM and TPM limiting with MySQL-backed usage persistence
- MySQL-backed `route_rules` for backend selection and model-specific priority
- deterministic provider fallback with passive health, active probes, and
  `/readyz` plus `/debug/providers`
- provider adapters for OpenAI-compatible, Anthropic, Ollama, and mock backends

### Stage 6: Observability

- `X-Request-Id` and `X-Trace-Id` on every HTTP response
- structured logs with request, trace, and span correlation
- Prometheus-style `/metrics`
- optional OTLP/HTTP trace export
- a local Prometheus, Grafana, and OTLP demo path with committed verification
  evidence

### Stage 7: Delivery and Verification

- Docker Compose local stack
- baseline Kubernetes manifests plus production and optional HPA overlays
- built-in load testing through `cmd/loadtest`
- repeatable failure drills for provider errors, timeouts, quota rejection, and
  streaming interruption
- nightly benchmark and drill regression workflow
- shared repository verification commands for static, runtime, observability,
  Kubernetes, and SonarCloud checks

## Start Here

Choose the shortest path for what you want to do next:

- understand the project in 10 minutes:
  [docs/quick-start-guide.md](docs/quick-start-guide.md)
- run the gateway locally:
  [docs/local-development.md](docs/local-development.md)
- understand the architecture and boundaries:
  [docs/architecture/overview.md](docs/architecture/overview.md)
- inspect routing and fallback behavior:
  [docs/architecture/routing-resilience.md](docs/architecture/routing-resilience.md)
- inspect observability design:
  [docs/architecture/observability.md](docs/architecture/observability.md)
- inspect the proof and verification trail:
  [docs/verification/README.md](docs/verification/README.md)

## Fast Local Path

If you want the quickest shell-based local path:

```bash
docker compose -f deployments/docker/docker-compose.yml up -d mysql redis
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'
go run ./cmd/devinit
go run ./cmd/gateway
curl -i http://127.0.0.1:8080/healthz
```

Expected local seed output from `go run ./cmd/devinit` includes:

- tenant: `local-dev`
- API key: `lag-local-dev-key`
- default route rules seeded from the current provider config

If you prefer a full container path instead of running the gateway in your
shell, use [docs/deployment/docker-compose.md](docs/deployment/docker-compose.md).

## Verification Entrypoints

These are the main commands that turn the repository into evidence rather than
just source files:

- `make stage7-static`
  validates tests, vet, deployment assets, dashboard JSON, and required Stage 7
  assets
- `make stage7-runtime`
  runs the smoke and built-in load contract against a live local gateway
- `make observability-demo-verify`
  proves the local metrics, OTLP export, collector, Prometheus, and Grafana
  loop
- `make k8s-production-local-check`
  validates the production and optional HPA overlays without requiring a real
  cluster
- `make k8s-production-server-dry-run`
  is an environment gate only, and should be run only when a real cluster
  exists
- `make sonar-quality-gate-check`
  verifies the external SonarCloud release gate on `main`

The full evidence map lives in
[docs/verification/README.md](docs/verification/README.md).

## Documentation Map

- project boundary and staged roadmap:
  [docs/execution-roadmap.md](docs/execution-roadmap.md)
- API shape and endpoint behavior:
  [docs/api.md](docs/api.md)
- local setup and troubleshooting:
  [docs/local-development.md](docs/local-development.md)
- deployment guidance:
  [docs/deployment/docker-compose.md](docs/deployment/docker-compose.md),
  [docs/deployment/kubernetes.md](docs/deployment/kubernetes.md)
- verification and readiness:
  [docs/verification/stage7-production-readiness.md](docs/verification/stage7-production-readiness.md)
- full docs entry:
  [docs/README.md](docs/README.md)

## Non-Goals

This repository intentionally does not try to be:

- a frontend chat UI
- a RAG or vector retrieval stack
- an agent workflow platform
- a model training or inference optimization system
- a generic AI demo repository

That boundary is what keeps the project coherent as infrastructure and gateway
engineering evidence.
