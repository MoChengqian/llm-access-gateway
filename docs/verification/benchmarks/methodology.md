# Benchmark Methodology

## Overview

This document explains how benchmark evidence was collected on `2026-04-07`, what was measured, and where the limits of the run are.

The benchmark strategy intentionally separated three questions:

1. what does the gateway cost when the provider path is fully in-process
2. what extra cost appears when the HTTP adapter path is enabled
3. what do the local resource snapshots look like during a longer non-stream run

## Environment

- OS timezone during collection: `Asia/Shanghai (CST)`
- Base URL: `http://127.0.0.1:8080`
- Gateway process: `go run ./cmd/gateway`
- MySQL: Docker container from `mysql:8.4`
- Redis: Docker container from `redis:7.4-alpine`
- Seed command: `go run ./cmd/devinit`
- Development tenant: `local-dev`
- API key: `lag-local-dev-key`

## Benchmark Commands

`cmd/loadtest` is the canonical Stage 7 load tool. `k6` is intentionally not
part of the v1 contract because the built-in command already covers the gateway's
auth header, OpenAI-compatible request shape, JSON response validation, SSE
stream validation, machine-readable output, and threshold checks used by nightly
automation.

### Headline Non-Stream Run

```bash
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 100 -concurrency 10 -json -min-success-rate 1.0
```

### Headline Stream Run

```bash
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 50 -concurrency 5 -stream -json -min-success-rate 1.0
```

### Extended Observation Run For Resource Sampling

```bash
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 2000 -concurrency 100 -json -min-success-rate 1.0
docker stats --no-stream llm-access-gateway-mysql llm-access-gateway-redis
ps -Ao pid,ppid,%cpu,%mem,rss,command | rg 'cmd/gateway|/tmp/go-build.*/exe/gateway'
```

## Limit Handling

The seeded development tenant defaults to:

- `rpm_limit=60`
- `tpm_limit=4000`
- `token_budget=1000000`

Those defaults are correct for local smoke checks, but they would distort the benchmark because the spec-level non-stream run uses `100` requests.

During evidence collection, tenant limits were temporarily raised in MySQL before the benchmark window and restored afterwards:

- headline benchmark window: `rpm_limit=1000`, `tpm_limit=100000`
- extended observation window: `rpm_limit=10000`, `tpm_limit=100000`
- final restored state: `rpm_limit=60`, `tpm_limit=4000`, `token_budget=1000000`

Redis limiter counters were flushed before quota drills and after evidence collection so the repo returned to its normal development posture.

## Comparison Modes

### Mode A: In-Process Mock Provider

This is the default local path:

- `provider.primary.type=mock`
- `provider.secondary.type=mock`

It measures the gateway stack without external HTTP adapter overhead.

### Mode B: OpenAI-Compatible HTTP Adapter Path

This mode switched the primary provider to `internal/provider/openai` and pointed it at a local synthetic upstream:

```bash
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode ok
export APP_PROVIDER_PRIMARY_TYPE='openai'
export APP_PROVIDER_PRIMARY_NAME='openai-primary'
export APP_PROVIDER_PRIMARY_BASE_URL='http://127.0.0.1:18081/v1'
export APP_PROVIDER_PRIMARY_MODEL='gpt-4o-mini'
export APP_PROVIDER_PRIMARY_TIMEOUT_SECONDS='1'
export APP_PROVIDER_PRIMARY_MAX_RETRIES='1'
export APP_PROVIDER_PRIMARY_RETRY_BACKOFF_MILLISECONDS='200'
```

This is the closest local approximation of a real provider path that was possible without external credentials. It exercises:

- HTTP request creation
- JSON serialization and decoding
- retry logic inside the OpenAI-compatible adapter
- router fallback on adapter failure

It does **not** include WAN latency or hosted-provider variability.

## Resource Sampling

The 100-request headline benchmark completed too quickly to make one-off CPU sampling meaningful, so a longer observation run was added:

```json
{
  "requests": 2000,
  "concurrency": 100,
  "stream": false,
  "total_duration_ms": 1784,
  "success": 2000,
  "failure": 0,
  "status_counts": {
    "200": 2000
  },
  "latency_p50_ms": 86,
  "latency_p95_ms": 121,
  "latency_max_ms": 165
}
```

Resource snapshots taken during that observation window were:

| Component | Snapshot |
|----------|----------|
| Gateway process RSS | `28528 KiB` |
| Gateway CPU sample | `0.0%` in one `ps` snapshot |
| Gateway memory sample | `0.2%` in one `ps` snapshot |
| MySQL | `3.00% CPU`, `587.8 MiB` |
| Redis | `1.65% CPU`, `9.953 MiB` |

The gateway CPU sample is not very informative because the request burst was short and a single `ps` sample can miss it. The MySQL and Redis container snapshots were still useful because they showed which dependency dominated local resource usage. In this local stack, MySQL memory footprint was substantially larger than Redis.

## Limitations

- Metrics are process-local and reset when the gateway restarts.
- The adapter-path comparison uses a local synthetic upstream, not a hosted provider.
- The load test tool reports P50, P95, and max; it does not currently emit histogram buckets or P99.
- Because the gateway process is short-lived in local verification, resource sampling is point-in-time rather than continuous.

## Nightly Baseline Automation

The repository now keeps a persisted nightly benchmark reference in:

```text
.github/nightly/benchmark-baseline.json
```

That baseline is derived from the verified local evidence captured on `2026-04-07`. The nightly workflow compares current benchmark artifacts against threshold checks, then renders a markdown delta summary so regressions are visible without manually opening each JSON file.

The Stage 7 entrypoint for static verification is:

```bash
./scripts/stage7-verify.sh static
```

Runtime smoke and load verification against a live gateway is:

```bash
./scripts/stage7-verify.sh runtime
```

## Related Documentation

- [Non-Streaming Benchmarks](non-streaming.md)
- [Streaming Benchmarks](streaming.md)
- [Stage 7 Delivery Contract](../stage7-delivery-contract.md)
- [Performance Benchmarking Article](../../blog/005-performance.md)
- [Local Development](../../local-development.md)

## Code References

- `cmd/loadtest/main.go`
- `cmd/gateway/main.go`
- `internal/service/governance/service.go`
- `internal/service/governance/redis_limiter.go`
- `internal/provider/mock/chat.go`
- `internal/provider/openai/chat.go`
- `deployments/docker/docker-compose.yml`
- `scripts/synthetic-openai-upstream.py`
- `cmd/nightlycheck/main.go`
- `cmd/nightlyreport/main.go`
- `.github/nightly/benchmark-baseline.json`
