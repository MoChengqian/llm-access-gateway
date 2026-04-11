# Stage 7 Delivery And Verification Contract

## Purpose

Stage 7 turns the gateway from a working codebase into an operationally
demonstrable system. The contract is not "we have scripts somewhere"; it is a
small set of repeatable commands that prove delivery, load, failure, and CI
assets are still aligned.

## Canonical Verification Entrypoints

### Static Contract

Run this before committing delivery, deployment, observability, load, or drill
changes:

```bash
./scripts/stage7-verify.sh static
make stage7-static
```

This validates:

- `go test ./...`
- `go vet ./...`
- Docker Compose expansion and Kubernetes manifest structure through
  `scripts/validate-deployments.rb`
- Grafana dashboard JSON syntax
- presence of the required Stage 7 delivery, load, drill, benchmark, and CI
  assets

### Runtime Contract

Run this against a live gateway with `lag-local-dev-key` seeded:

```bash
./scripts/stage7-verify.sh runtime
make stage7-runtime
make verify
```

This executes `ASSERT=true ./scripts/gateway-smoke-check.sh`, which covers:

- `/healthz`
- `/metrics`
- `/v1/models`
- `/v1/usage`
- non-stream chat completions
- stream chat completions
- non-stream built-in load test
- stream built-in load test

### Full Local Contract

If a local gateway is already running:

```bash
./scripts/stage7-verify.sh all
make stage7-verify
```

## Load Tooling Decision

`cmd/loadtest` remains the canonical load tool for Stage 7.

Reasons:

- it exercises the repository's real auth header, OpenAI-compatible request
  shape, and SSE response contract
- it emits machine-readable JSON consumed by `cmd/nightlycheck` and
  `cmd/nightlyreport`
- it supports both non-stream and stream modes with success-rate, latency P95,
  and TTFT P95 thresholds
- it has no external runtime dependency beyond Go

`k6` is intentionally not added in Stage 7. It should only be introduced later
if it proves a materially different contract, such as distributed load,
externally hosted execution, richer scenario composition, or long-duration
resource trend collection that the built-in tool cannot cover.

## Failure Drill Contract

The repository keeps failure drills as documented, repeatable evidence rather
than ad-hoc terminal history.

Canonical drills:

- provider error fallback:
  `docs/verification/failure-drills/provider-errors.md`
- provider timeout fallback:
  `docs/verification/failure-drills/provider-timeout.md`
- quota rejection:
  `docs/verification/failure-drills/quota-enforcement.md`
- streaming pre-chunk fallback and partial-stream interruption:
  `docs/verification/failure-drills/streaming-failures.md`
- Anthropic adapter translation and streaming behavior:
  `docs/verification/failure-drills/anthropic-adapter.md`

Automation support:

- `scripts/provider-fallback-drill.sh`
- `scripts/anthropic-adapter-drill.sh`
- `cmd/nightlycheck`
- `cmd/nightlyreport`
- `.github/workflows/nightly-verification.yml`

## Workflow Contract

Stage 7 CI must preserve the primary failing signal instead of obscuring it with
secondary workflow noise.

That means:

- official GitHub Actions are pinned to Node 24-ready major versions
- the nightly `report` job only downloads artifacts from prerequisite jobs that
  actually succeeded
- when a prerequisite job fails or is skipped, the nightly summary degrades into
  a readable markdown report instead of introducing a second failure caused only
  by missing artifacts

## Deployment Contract

Delivery assets are split by runtime target:

- Docker Compose local stack:
  `deployments/docker/docker-compose.yml`
- Kubernetes baseline:
  `deployments/k8s/*`
- Kubernetes production overlay:
  `deployments/k8s-overlays/production/*`
- structural validation:
  `scripts/validate-deployments.rb`

The Stage 7 static contract validates that these assets remain parseable and
structurally aligned. The production overlay is also rendered when `kubectl` is
available, so ingress, PDB, pod security, provider config, Secret patches, and
image overrides are checked as one delivery bundle. Cluster-specific MySQL,
Redis, image registry credentials, TLS issuance, and collector deployment remain
environment-owned.

The local production overlay evidence is recorded in
[`k8s-production-overlay.md`](k8s-production-overlay.md).

## Benchmark Contract

Benchmark methodology and result documents live under:

```text
docs/verification/benchmarks/
```

The persisted comparison baseline lives at:

```text
.github/nightly/benchmark-baseline.json
```

Nightly verification must keep using `cmd/loadtest` JSON outputs as the source
of truth, then check them through `cmd/nightlycheck` and render summaries through
`cmd/nightlyreport`.

## Stage 7 Completion Criteria

Stage 7 is complete when all of the following are true:

- static delivery contract passes
- runtime smoke/load contract passes against a live gateway
- Docker Compose and Kubernetes assets are validated by one shared entrypoint
- load tooling has a declared canonical path
- failure drills are documented and mapped to automation
- CI and nightly verification use the same contract instead of drifting into
  separate definitions
- nightly reporting preserves root-cause visibility when prerequisite jobs fail
