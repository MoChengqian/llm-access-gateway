# Non-Streaming Benchmark Report

## Overview

This report captures the `POST /v1/chat/completions` non-stream baseline on `2026-04-07`.

Two provider modes were measured:

- the default in-process mock provider path
- the OpenAI-compatible HTTP adapter path using a local synthetic upstream

The goal was to separate gateway overhead from provider-adapter overhead while keeping the rest of the stack unchanged.

## Test Environment

- Date: `2026-04-07`
- Base URL: `http://127.0.0.1:8080`
- Gateway process: `go run ./cmd/gateway`
- Data stores: MySQL and Redis started from `deployments/docker/docker-compose.yml`
- Auth seed: `go run ./cmd/devinit`
- API key: `lag-local-dev-key`
- Tenant limits during benchmark: temporarily raised above the benchmark request volume to avoid quota distortion

## Test Configuration

### Mock Provider Path

```bash
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 100 -concurrency 10 -json -min-success-rate 1.0
```

### OpenAI-Compatible HTTP Adapter Path

```bash
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode ok

export APP_PROVIDER_PRIMARY_TYPE='openai'
export APP_PROVIDER_PRIMARY_NAME='openai-primary'
export APP_PROVIDER_PRIMARY_BASE_URL='http://127.0.0.1:18081/v1'
export APP_PROVIDER_PRIMARY_MODEL='gpt-4o-mini'
export APP_PROVIDER_PRIMARY_TIMEOUT_SECONDS='1'
export APP_PROVIDER_PRIMARY_MAX_RETRIES='1'
export APP_PROVIDER_PRIMARY_RETRY_BACKOFF_MILLISECONDS='200'

go run ./cmd/gateway
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 100 -concurrency 10 -json -min-success-rate 1.0
```

## Results

| Metric | Mock Provider | HTTP Adapter Path |
|--------|---------------|-------------------|
| Requests | 100 | 100 |
| Concurrency | 10 | 10 |
| Success | 100 | 100 |
| Failure | 0 | 0 |
| Approx QPS | 740.7 req/s | 609.8 req/s |
| Latency P50 | 11 ms | 14 ms |
| Latency P95 | 22 ms | 34 ms |
| Latency Max | 24 ms | 37 ms |
| Total Duration | 135 ms | 164 ms |

## Raw Output

### Mock Provider

```json
{
  "requests": 100,
  "concurrency": 10,
  "stream": false,
  "total_duration_ms": 135,
  "success": 100,
  "failure": 0,
  "status_counts": {
    "200": 100
  },
  "latency_p50_ms": 11,
  "latency_p95_ms": 22,
  "latency_max_ms": 24
}
```

### HTTP Adapter Path

```json
{
  "requests": 100,
  "concurrency": 10,
  "stream": false,
  "total_duration_ms": 164,
  "success": 100,
  "failure": 0,
  "status_counts": {
    "200": 100
  },
  "latency_p50_ms": 14,
  "latency_p95_ms": 34,
  "latency_max_ms": 37
}
```

## Analysis

The mock path is the cheapest possible local route: auth, governance, routing, and response shaping happen inside one process and the provider work is in-memory. That produced a stable `100/100` success rate and a `22 ms` P95.

Switching the primary backend to the OpenAI-compatible adapter added real HTTP request construction, JSON encode/decode, and socket round-trips, even though the upstream was still local. That raised P95 from `22 ms` to `34 ms` and lowered throughput from `~741 req/s` to `~610 req/s`.

That difference is useful because it isolates adapter overhead without mixing in WAN latency. It should not be read as a benchmark against a hosted provider. For that, use the same commands with `APP_PROVIDER_PRIMARY_BASE_URL` and `APP_PROVIDER_PRIMARY_API_KEY` pointing at a live upstream.

## Reproduction

```bash
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 100 -concurrency 10 -json -min-success-rate 1.0

python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode ok
export APP_PROVIDER_PRIMARY_TYPE='openai'
export APP_PROVIDER_PRIMARY_NAME='openai-primary'
export APP_PROVIDER_PRIMARY_BASE_URL='http://127.0.0.1:18081/v1'
export APP_PROVIDER_PRIMARY_MODEL='gpt-4o-mini'
export APP_PROVIDER_PRIMARY_TIMEOUT_SECONDS='1'
export APP_PROVIDER_PRIMARY_MAX_RETRIES='1'
export APP_PROVIDER_PRIMARY_RETRY_BACKOFF_MILLISECONDS='200'
go run ./cmd/gateway
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 100 -concurrency 10 -json -min-success-rate 1.0
```

## Related Documentation

- [Streaming Benchmarks](streaming.md)
- [Benchmark Methodology](methodology.md)
- [Performance Benchmarking Article](../../blog/005-performance.md)

## Code References

- `cmd/loadtest/main.go`
- `cmd/gateway/main.go`
- `internal/provider/mock/chat.go`
- `internal/provider/openai/chat.go`
- `scripts/synthetic-openai-upstream.py`
