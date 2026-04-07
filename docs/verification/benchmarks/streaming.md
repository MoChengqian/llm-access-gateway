# Streaming Benchmark Report

## Overview

This report captures the `POST /v1/chat/completions` streaming baseline on `2026-04-07`.

The benchmark focuses on two user-visible questions:

- how quickly does the first chunk arrive
- how much total latency is added by the provider path behind the stream

As with the non-stream benchmark, the run compares the default in-process mock provider path with the OpenAI-compatible HTTP adapter path backed by a local synthetic upstream.

## Test Environment

- Date: `2026-04-07`
- Base URL: `http://127.0.0.1:8080`
- Gateway process: `go run ./cmd/gateway`
- Data stores: MySQL and Redis from `deployments/docker/docker-compose.yml`
- Auth seed: `go run ./cmd/devinit`
- API key: `lag-local-dev-key`
- Tenant limits during benchmark: temporarily raised above the benchmark request volume

## Test Configuration

### Mock Provider Path

```bash
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 50 -concurrency 5 -stream -json -min-success-rate 1.0
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
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 50 -concurrency 5 -stream -json -min-success-rate 1.0
```

## Results

| Metric | Mock Provider | HTTP Adapter Path |
|--------|---------------|-------------------|
| Requests | 50 | 50 |
| Concurrency | 5 | 5 |
| Success | 50 | 50 |
| Failure | 0 | 0 |
| Approx Throughput | 549.5 req/s | 61.4 req/s |
| Latency P50 | 7 ms | 79 ms |
| Latency P95 | 19 ms | 98 ms |
| Latency Max | 20 ms | 99 ms |
| TTFT P50 | 4 ms | 6 ms |
| TTFT P95 | 12 ms | 28 ms |
| TTFT Max | 15 ms | 30 ms |
| Stream Chunks | 200 | 150 |
| Total Duration | 91 ms | 814 ms |

## Raw Output

### Mock Provider

```json
{
  "requests": 50,
  "concurrency": 5,
  "stream": true,
  "total_duration_ms": 91,
  "success": 50,
  "failure": 0,
  "status_counts": {
    "200": 50
  },
  "latency_p50_ms": 7,
  "latency_p95_ms": 19,
  "latency_max_ms": 20,
  "ttft_p50_ms": 4,
  "ttft_p95_ms": 12,
  "ttft_max_ms": 15,
  "stream_chunks_total": 200
}
```

### HTTP Adapter Path

```json
{
  "requests": 50,
  "concurrency": 5,
  "stream": true,
  "total_duration_ms": 814,
  "success": 50,
  "failure": 0,
  "status_counts": {
    "200": 50
  },
  "latency_p50_ms": 79,
  "latency_p95_ms": 98,
  "latency_max_ms": 99,
  "ttft_p50_ms": 6,
  "ttft_p95_ms": 28,
  "ttft_max_ms": 30,
  "stream_chunks_total": 150
}
```

## Analysis

The first important observation is that TTFT stayed low in both modes. Even through the HTTP adapter path, the first chunk still arrived with a `6 ms` P50 and `28 ms` P95 in this local environment.

The second observation is that total stream latency diverged much more sharply than TTFT. That is expected. The synthetic HTTP upstream emits chunked SSE with small inter-chunk delays, so the adapter-path run spends most of its extra time after the first token, not before it.

That makes the current stream benchmark useful for two reasons:

- it confirms that the gateway preserves low local TTFT when the upstream opens quickly
- it shows that full-stream completion time tracks upstream pacing more than gateway overhead

As with the non-stream report, this is not a hosted-provider benchmark. It is an adapter-path comparison that keeps WAN effects out of the measurement.

## Reproduction

```bash
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 50 -concurrency 5 -stream -json -min-success-rate 1.0

python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode ok
export APP_PROVIDER_PRIMARY_TYPE='openai'
export APP_PROVIDER_PRIMARY_NAME='openai-primary'
export APP_PROVIDER_PRIMARY_BASE_URL='http://127.0.0.1:18081/v1'
export APP_PROVIDER_PRIMARY_MODEL='gpt-4o-mini'
export APP_PROVIDER_PRIMARY_TIMEOUT_SECONDS='1'
export APP_PROVIDER_PRIMARY_MAX_RETRIES='1'
export APP_PROVIDER_PRIMARY_RETRY_BACKOFF_MILLISECONDS='200'
go run ./cmd/gateway
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 50 -concurrency 5 -stream -json -min-success-rate 1.0
```

## Related Documentation

- [Non-Streaming Benchmarks](non-streaming.md)
- [Benchmark Methodology](methodology.md)
- [SSE Streaming Implementation](../../blog/002-sse-streaming.md)
- [Performance Benchmarking Article](../../blog/005-performance.md)

## Code References

- `cmd/loadtest/main.go`
- `internal/api/handlers/chat.go`
- `internal/service/chat/service.go`
- `internal/provider/router/chat.go`
- `internal/provider/openai/chat.go`
- `scripts/synthetic-openai-upstream.py`
