# Production Considerations

## Overview

The current repository provides the building blocks for production-style deployment, but it is still intentionally small. This document focuses on the operational behaviors that matter most when deploying the gateway beyond local development:

- liveness and readiness semantics
- dependency startup and bootstrap ordering
- provider health and cooldown behavior
- observability endpoints
- practical rollout and verification guidance

## Health Endpoints

### `/healthz`

`/healthz` is a liveness endpoint that always returns:

```json
{"status":"ok"}
```

Use it to answer a narrow question: is the HTTP process alive and able to respond?

### `/readyz`

`/readyz` checks aggregate provider readiness. It returns:

- `200` with `{"status":"ready"}` when at least one backend is healthy
- `503` with `{"status":"not ready"}` when all backends are unhealthy

This makes `/readyz` the correct probe for:

- Kubernetes readiness probes
- load balancer traffic admission
- rollout gates

### `/debug/providers`

`/debug/providers` is the operator-facing detail view for readiness. It exposes:

- aggregate `ready` state
- each backend name
- whether each backend is currently healthy
- consecutive failure count
- `unhealthy_until`
- last probe time and last probe error

In production-like environments, this endpoint is the first place to inspect when `/readyz` flips from `200` to `503`.

## Dependency Ordering

The gateway has a hard dependency on MySQL and an optional-but-preferred dependency on Redis.

### MySQL

MySQL is required because:

- API key authentication uses MySQL
- request usage tracking uses MySQL
- the process exits at startup when `mysql.dsn` is empty or unreachable

### Redis

Redis is optional in the sense that request handling still works without it, but the limiter behavior changes:

- with Redis available, RPM/TPM counters are Redis-backed
- when Redis ping fails, the gateway falls back to the MySQL limiter and logs the fallback

### Bootstrap step

Before serving traffic, run `devinit` (or an equivalent migration/bootstrap job) so the schema and initial tenant/API key records exist.

In this repo:

- Docker Compose uses a dedicated `devinit` service
- Kubernetes uses `Job/llm-access-gateway-devinit`

## Provider Failure and Cooldown Behavior

The provider router keeps in-memory health state:

- a backend becomes unhealthy after `provider_failure_threshold` consecutive failures
- the backend remains skipped until `provider_cooldown_seconds` elapses
- successful requests or successful probes clear the unhealthy state

Operational implications:

- readiness can change even while the gateway process remains alive
- if both providers enter cooldown, `/readyz` becomes `503`
- the request path still makes a best-effort attempt once all backends are unhealthy, but traffic admission should rely on `/readyz`

For stream requests, fallback is only allowed before the first chunk arrives from upstream. After the first chunk, interruptions are terminal for that response.

## Observability Endpoints and Signals

Use these endpoints as your basic operational surface:

- `GET /healthz`
- `GET /readyz`
- `GET /debug/providers`
- `GET /metrics`

The gateway also returns:

- `X-Request-Id`
- `X-Trace-Id`

and emits structured logs plus optional OTLP spans with:

- `request_id`
- `trace_id`

Enable OTLP trace export by setting
`APP_OBSERVABILITY_OTLP_TRACES_ENDPOINT` to an OTLP/HTTP collector URL such as
`http://otel-collector:4318/v1/traces`. Import
`deployments/grafana/dashboards/llm-access-gateway.json` when a Prometheus
scrape path for `/metrics` is available.
- `span_id`
- provider routing events

That means a production debugging loop can be:

1. capture `X-Request-Id` and `X-Trace-Id`
2. inspect logs for the correlated request
3. inspect `/debug/providers` for backend health state
4. inspect `/metrics` for aggregate trends such as readiness failures or governance rejections

## Resource and Scaling Notes

The baseline Kubernetes deployment requests and limits:

- request: `100m` CPU / `128Mi` memory
- limit: `500m` CPU / `256Mi` memory

Those values are baseline examples, not capacity guarantees. Before scaling traffic, validate:

- latency under your expected concurrency
- stream TTFT behavior
- provider timeout behavior
- MySQL and Redis capacity outside the gateway itself

Because health state is kept in-process today, multiple replicas will not share cooldown counters or probe state. That is acceptable for a baseline deployment, but it is an important architectural limit to remember.

The production overlay in
`deployments/k8s-overlays/production/` raises the gateway Deployment to two
replicas, adds a `PodDisruptionBudget`, applies stricter pod/container security
defaults, sets `imagePullPolicy: Always`, annotates pods for Prometheus
scraping, and adds a `NetworkPolicy` for ingress and egress boundaries. Treat
those settings as a stronger starting point, not a substitute for
environment-specific capacity tests.

The optional overlay in `deployments/k8s-overlays/production-hpa/` adds
`HorizontalPodAutoscaler/llm-access-gateway` with two to six replicas and a 70%
CPU target. Use it only when the cluster has metrics support and after checking
provider quotas, MySQL capacity, and Redis capacity.

## Verification Before and After Rollout

For the end-to-end readiness matrix, start with
[`../verification/stage7-production-readiness.md`](../verification/stage7-production-readiness.md).

### Pre-rollout checks

```bash
./scripts/stage7-verify.sh static
make stage7-static
```

If you have cluster access, also verify the Kubernetes apply flow in your target environment before the first rollout.

For the production overlay:

```bash
make k8s-production-render
make k8s-production-hpa-render
make k8s-production-local-check
make k8s-production-server-dry-run
kubectl apply -k deployments/k8s-overlays/production
kubectl -n llm-access-gateway rollout status deployment/llm-access-gateway --timeout=180s
```

`make k8s-production-server-dry-run` requires a reachable target cluster. Run it
before real apply so API compatibility, HPA metrics support, and server-side
schema validation fail before rollout time.

### Post-rollout checks

```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
curl -i http://127.0.0.1:8080/debug/providers
curl -i http://127.0.0.1:8080/metrics
./scripts/gateway-smoke-check.sh
ASSERT=true ./scripts/gateway-smoke-check.sh
./scripts/stage7-verify.sh runtime
```

For resilience drills:

```bash
./scripts/provider-fallback-drill.sh create-fail
./scripts/provider-fallback-drill.sh stream-fail
```

## Practical Recommendations

- Keep handlers thin and operational behavior centered in services and middleware, matching the current codebase shape.
- Put secrets in environment variables or Kubernetes Secrets, not in committed config files.
- Use `/readyz` for readiness and `/healthz` for liveness; do not swap them.
- Run bootstrap initialization before traffic cutover.
- Keep the secondary backend configured and healthy if fallback matters to your rollout.
- Re-run smoke checks after changing provider, auth, governance, or health-related settings.

## Current Limitations

- No production-grade distributed tracing backend is provisioned by the gateway manifests.
- Health and cooldown state is process-local and resets on restart.
- The production overlay includes ingress, NetworkPolicy, PDB, and optional HPA wiring, but persistent Redis/MySQL resources, TLS issuance, registry credentials, provider egress policy, and collector deployment remain environment-owned.
- Provider routing is ordered failover, not weighted balancing.

## Related Documentation

- [Docker Compose Deployment](docker-compose.md)
- [Kubernetes Deployment](kubernetes.md)
- [Configuration Reference](configuration.md)
- [Observability Design](../architecture/observability.md)
- [Routing and Resilience](../architecture/routing-resilience.md)

## Code References

- [`internal/api/handlers/health.go`](../../internal/api/handlers/health.go)
- [`internal/provider/router/chat.go`](../../internal/provider/router/chat.go)
- [`cmd/gateway/main.go`](../../cmd/gateway/main.go)
- [`deployments/docker/docker-compose.yml`](../../deployments/docker/docker-compose.yml)
- [`deployments/k8s/deployment.yaml`](../../deployments/k8s/deployment.yaml)
- [`scripts/gateway-smoke-check.sh`](../../scripts/gateway-smoke-check.sh)
