# Verification Index

This directory is the proof map for the current repository contract. It tells
you which command to run, which document to read, and what the result actually
proves.

Use this page when you want to answer one of these questions quickly:

- how do I verify Stage 5 routing and governance behavior
- how do I verify Stage 6 observability behavior
- how do I verify Stage 7 delivery, load, and drill assets
- which checks are repository-owned and which checks need a real environment

## Recommended Verification Order

1. run the Stage 7 static contract
2. run the runtime smoke and built-in load contract
3. inspect routing and fallback drills
4. inspect observability export and dashboard verification
5. inspect Kubernetes local render checks
6. run cluster dry-run only if a real cluster exists

## Stage 5: Routing, Governance, And Fallback

### What It Proves

Stage 5 proves the gateway is no longer just a config-wired proxy:

- API key authentication is tenant-scoped and MySQL-backed
- quota enforcement and usage recording are in the request path
- backend choice can be driven by persisted MySQL `route_rules`
- fallback is deterministic and health-aware
- streaming fallback stops after the first successful chunk

### Primary Commands

```bash
go run ./cmd/routerulectl list
./scripts/gateway-smoke-check.sh
./scripts/provider-fallback-drill.sh create-fail
./scripts/provider-fallback-drill.sh stream-fail
```

### Read This Evidence

- [Routing & Resilience](../architecture/routing-resilience.md)
- [Authentication](../api/authentication.md)
- [Provider Error Drill](failure-drills/provider-errors.md)
- [Streaming Failure Drill](failure-drills/streaming-failures.md)
- [Quota Enforcement Drill](failure-drills/quota-enforcement.md)

### Boundary

This stage proves deterministic routing and governance behavior in the gateway.
It does not prove weighted traffic shaping, cross-region routing, or external
control-plane rollout workflows.

## Stage 6: Observability

### What It Proves

Stage 6 proves the repository can expose and verify a usable observability
contract:

- request and trace identifiers are propagated through the request path
- structured logs preserve request, trace, and span correlation
- `/metrics` exposes the gateway's operational counters
- OTLP/HTTP export can be tested locally
- a local collector, Prometheus, and Grafana loop exists as repository-owned
  evidence

### Primary Commands

```bash
./scripts/otlp-export-check.sh
make observability-demo-verify
curl -i http://127.0.0.1:8080/metrics
```

### Read This Evidence

- [Observability Design](../architecture/observability.md)
- [OTLP Export Verification](otlp-export.md)
- [Observability Demo Runtime](observability-demo-runtime.md)

### Boundary

This stage proves a local, reproducible observability path. It does not prove a
production trace store, long-term metrics retention, or environment-owned
Grafana operations.

## Stage 7: Delivery, Load, And Readiness

### What It Proves

Stage 7 proves the repository ships more than source code:

- Docker Compose, Kubernetes manifests, and overlays are kept aligned
- built-in load tooling and failure drills are repeatable
- nightly verification protects the main evidence loop
- release quality also depends on an external SonarCloud gate

### Primary Commands

```bash
make stage7-static
make stage7-runtime
make k8s-production-local-check
make sonar-quality-gate-check
```

### Read This Evidence

- [Stage 7 Delivery Contract](stage7-delivery-contract.md)
- [Stage 7 Production Readiness](stage7-production-readiness.md)
- [Benchmark Methodology](benchmarks/methodology.md)
- [Anthropic Adapter Drill](failure-drills/anthropic-adapter.md)

### Boundary

These checks prove the repository contract. They do not automatically prove any
specific production cluster is rollout-ready.

## Kubernetes Local Versus Environment Checks

There are two different Kubernetes gates:

- local repository gate:
  `make k8s-production-local-check`
- environment rollout gate:
  `make k8s-production-server-dry-run`

Run the server-side dry-run only when:

- a real cluster exists
- valid cluster credentials exist
- environment-owned values are ready to replace placeholders

Without a real cluster, the repository can still be Stage 7 complete. The
cluster gate remains pending by definition.

## Fastest Entry If You Are Reviewing The Repo

If you only want the shortest proof path, read these three documents:

1. [Stage 7 Delivery Contract](stage7-delivery-contract.md)
2. [Stage 7 Production Readiness](stage7-production-readiness.md)
3. [Routing & Resilience](../architecture/routing-resilience.md)

That set gives you the delivery contract, the rollout boundary, and the Stage 5
technical heart of the repository.
