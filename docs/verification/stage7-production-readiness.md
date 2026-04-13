# Stage 7 Production Readiness Matrix

Date: 2026-04-13

## Purpose

This page is the Stage 7 production-readiness entrypoint. It connects the
repository's delivery, observability, Kubernetes, smoke, load, drill, and
nightly evidence into one checklist.

It is not a claim that any arbitrary cluster is production-ready by default.
Cluster credentials, DNS, TLS, registry access, MySQL, Redis, provider quota,
NetworkPolicy enforcement, and observability retention remain environment-owned.

It also distinguishes repository completion from environment rollout
acceptance. The repository can complete Stage 7 before any real cluster
exists; a specific cluster only becomes rollout-ready after environment-owned
checks pass.

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
| Kubernetes server dry-run | [`k8s-production-cluster-checklist.md`](k8s-production-cluster-checklist.md), [`k8s-production-server-dry-run-2026-04-12.md`](k8s-production-server-dry-run-2026-04-12.md) | `make k8s-production-server-dry-run` | Environment gate before real apply |
| Load baseline | [`benchmarks/methodology.md`](benchmarks/methodology.md) | `go run ./cmd/loadtest ... -json` | Canonical load tool |
| Failure drills | [`failure-drills/provider-errors.md`](failure-drills/provider-errors.md) | `./scripts/provider-fallback-drill.sh create-fail` | Repeatable resilience evidence |
| Streaming drills | [`failure-drills/streaming-failures.md`](failure-drills/streaming-failures.md) | `./scripts/provider-fallback-drill.sh stream-fail` | Repeatable streaming evidence |
| Anthropic adapter drills | [`failure-drills/anthropic-adapter.md`](failure-drills/anthropic-adapter.md) | `./scripts/anthropic-adapter-drill.sh` | Adapter compatibility evidence |
| Nightly regression | [`benchmarks/methodology.md`](benchmarks/methodology.md) | `.github/workflows/nightly-verification.yml` | Scheduled regression guard |
| SonarCloud main quality gate | [`sonar-quality-gate.md`](sonar-quality-gate.md) | `make sonar-quality-gate-check` | Post-merge release gate |

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

## External Quality Gate

Run this after merge, before tagging a release, and any time SonarCloud reports
`neutral` instead of an explicit gate result:

```bash
make sonar-quality-gate-check
```

This check calls SonarCloud's public API and turns an ambiguous UI state into an
explicit result:

- `OK`: the assigned quality gate was computed successfully
- `ERROR` or `WARN`: the quality gate ran and failed a condition
- `NONE`: SonarCloud analyzed the branch but did not compute a quality gate

For this repository, `NONE` on `main` is not a code regression by itself. It is
an external project-configuration gap that must be fixed in SonarCloud by a
project admin. The canonical remediation path is documented in
[`sonar-quality-gate.md`](sonar-quality-gate.md).

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

## Kubernetes Repository Gate

Run local render checks first:

```bash
make k8s-production-local-check
```

This is the repository-owned Kubernetes completion gate. It proves the shipped
overlays still render, include the expected objects, and can be reviewed before
any environment exists.

## Kubernetes Environment Gate

Run target-cluster dry-run checks only when you have chosen a real cluster and
have valid cluster credentials:

```bash
make k8s-production-server-dry-run
```

This gate proves the target API server accepts the overlays and that HPA metrics
support is available before the optional HPA overlay is applied.

If no real cluster exists yet, this gate remains pending by definition. That is
not a repository defect.

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

## Repository Completion Decision

For the v1 repository contract, Stage 7 is complete when:

- `make stage7-static` passes with required Kubernetes render enabled
- `make stage7-runtime` passes against a live gateway
- `make observability-demo-verify` passes in a local demo environment
- `make k8s-production-local-check` passes locally
- nightly benchmark and failure-drill workflows remain green
- SonarCloud computes an explicit quality-gate result for `main`

If any of those fail, the project is not ready to claim the Stage 7 repository
contract.

## Environment Promotion Decision

A specific cluster environment is rollout-ready only when all of the following
are true:

- valid cluster access exists for the intended environment
- `make k8s-production-server-dry-run` passes against that cluster
- environment-owned values are replaced before apply
- post-apply rollout and smoke checks pass in that environment

Without a real cluster, the repository can still be Stage 7 complete, but no
cluster-specific rollout claim should be made.
