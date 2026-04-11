# Stage 7 Production Readiness Matrix

Date: 2026-04-11

## Purpose

This page is the Stage 7 production-readiness entrypoint. It connects the
repository's delivery, observability, Kubernetes, smoke, load, drill, and
nightly evidence into one checklist.

It is not a claim that any arbitrary cluster is production-ready by default.
Cluster credentials, DNS, TLS, registry access, MySQL, Redis, provider quota,
NetworkPolicy enforcement, and observability retention remain environment-owned.

## Readiness Matrix

| Area | Repository Evidence | Command | Status |
| --- | --- | --- | --- |
| Static contract | [`stage7-delivery-contract.md`](stage7-delivery-contract.md) | `make stage7-static` | Required before merge |
| Runtime smoke/load | [`stage7-delivery-contract.md`](stage7-delivery-contract.md) | `make stage7-runtime` | Required against live gateway |
| Docker Compose | [`../deployment/docker-compose.md`](../deployment/docker-compose.md) | `docker compose -f deployments/docker/docker-compose.yml config` | Covered by deployment validator |
| Observability demo | [`observability-demo-runtime.md`](observability-demo-runtime.md) | `make observability-demo-verify` | Local runtime evidence |
| OTLP export | [`otlp-export.md`](otlp-export.md) | `./scripts/otlp-export-check.sh` | Local trace export evidence |
| Kubernetes production render | [`k8s-production-overlay.md`](k8s-production-overlay.md) | `make k8s-production-render` | Required in CI |
| Kubernetes optional HPA render | [`k8s-production-overlay.md`](k8s-production-overlay.md) | `make k8s-production-hpa-render` | Required in CI |
| Kubernetes local preflight | [`k8s-production-cluster-checklist.md`](k8s-production-cluster-checklist.md) | `make k8s-production-local-check` | Required before cluster dry-run |
| Kubernetes server dry-run | [`k8s-production-cluster-checklist.md`](k8s-production-cluster-checklist.md) | `make k8s-production-server-dry-run` | Required before real apply |
| Load baseline | [`benchmarks/methodology.md`](benchmarks/methodology.md) | `go run ./cmd/loadtest ... -json` | Canonical load tool |
| Failure drills | [`failure-drills/provider-errors.md`](failure-drills/provider-errors.md) | `./scripts/provider-fallback-drill.sh create-fail` | Repeatable resilience evidence |
| Streaming drills | [`failure-drills/streaming-failures.md`](failure-drills/streaming-failures.md) | `./scripts/provider-fallback-drill.sh stream-fail` | Repeatable streaming evidence |
| Anthropic adapter drills | [`failure-drills/anthropic-adapter.md`](failure-drills/anthropic-adapter.md) | `./scripts/anthropic-adapter-drill.sh` | Adapter compatibility evidence |
| Nightly regression | [`benchmarks/methodology.md`](benchmarks/methodology.md) | `.github/workflows/nightly-verification.yml` | Scheduled regression guard |

## Minimum Merge Gate

Run this before merging delivery, routing, observability, load, drill, or
Kubernetes changes:

```bash
REQUIRE_K8S_PRODUCTION_RENDER=true make stage7-static
actionlint .github/workflows/runtime-ci.yml .github/workflows/nightly-verification.yml
git diff --check
```

This gate proves:

- Go tests and vet pass
- deployment assets remain structurally valid
- production and optional HPA overlays render with `kubectl`
- Grafana dashboard JSON parses
- Stage 7 required assets are still present
- workflow syntax and shell snippets remain lint-clean
- whitespace hygiene is clean

## Local Runtime Gate

Run this after starting a local gateway with seeded development data:

```bash
make stage7-runtime
```

For a full local observability loop:

```bash
make observability-demo-verify
```

These checks prove local request handling, auth, usage, load, metrics, and OTLP
export remain demonstrable without a target Kubernetes cluster.

## Kubernetes Gate

Run local render checks first:

```bash
make k8s-production-local-check
```

Run target-cluster dry-run checks before real apply:

```bash
make k8s-production-server-dry-run
```

This gate proves the target API server accepts the overlays and that HPA metrics
support is available before the optional HPA overlay is applied.

## Real Apply Gate

Before real apply, replace environment-owned values:

- image registry and tag
- ingress host and TLS secret
- MySQL DSN
- Redis password and address
- provider API keys
- OTLP collector service URL
- NetworkPolicy namespace selectors and provider egress policy

Then apply and verify:

```bash
kubectl apply -k deployments/k8s-overlays/production
kubectl -n llm-access-gateway wait --for=condition=complete job/llm-access-gateway-devinit --timeout=120s
kubectl -n llm-access-gateway rollout status deployment/llm-access-gateway --timeout=180s
kubectl -n llm-access-gateway get deploy,svc,ingress,networkpolicy,pdb
```

Apply the optional HPA overlay only when metrics support is confirmed:

```bash
kubectl apply -k deployments/k8s-overlays/production-hpa
kubectl -n llm-access-gateway get hpa llm-access-gateway
```

## Explicit Non-Claims

- This repository does not provision production MySQL or Redis.
- This repository does not provision DNS, TLS issuance, or registry credentials.
- This repository does not prove NetworkPolicy enforcement unless the target CNI enforces it.
- This repository does not provide long-term metrics or trace retention.
- This repository does not replace provider-side quota, billing, or incident controls.

## Readiness Decision

For the v1 repository contract, Stage 7 is complete when:

- `make stage7-static` passes with required Kubernetes render enabled
- `make stage7-runtime` passes against a live gateway
- `make observability-demo-verify` passes in a local demo environment
- `make k8s-production-local-check` passes locally
- `make k8s-production-server-dry-run` passes against the target cluster before real apply
- nightly benchmark and failure-drill workflows remain green

If any of those fail, the project is not ready to claim production-style
delivery for that environment.
