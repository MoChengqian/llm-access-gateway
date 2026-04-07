# 005: Benchmarking A Gateway Means Measuring The Right Layer

## Overview

Performance numbers without context are easy to collect and hard to trust.

This gateway is a good example. If you only measure the default mock provider path, you mostly learn about auth, governance, routing, and JSON shaping inside one process. If you switch to an HTTP-backed provider path, you start paying for socket I/O and JSON encode/decode as well.

Both measurements are useful, but they answer different questions.

## The Built-In Load Test Is Small And Useful

The repo includes `cmd/loadtest`, which is intentionally narrow:

- it drives `POST /v1/chat/completions`
- it supports non-stream and stream modes
- it emits success and failure counts
- it reports latency P50, P95, max
- in stream mode it also reports TTFT and total chunk count

That makes it a practical verification tool, not just a demo binary.

## What The Non-Stream Benchmark Showed

On `2026-04-07`, the non-stream benchmark was run with `100` requests at concurrency `10`.

| Path | Success | Failure | Approx QPS | P50 | P95 | Max |
|------|---------|---------|------------|-----|-----|-----|
| In-process mock | 100 | 0 | 740.7 req/s | 11 ms | 22 ms | 24 ms |
| Local HTTP adapter | 100 | 0 | 609.8 req/s | 14 ms | 34 ms | 37 ms |

The difference is exactly what you would expect: the HTTP adapter path adds real request construction and response parsing, even though the upstream was still local.

## What The Stream Benchmark Showed

The streaming benchmark was run with `50` requests at concurrency `5`.

| Path | Success | Failure | Throughput | Latency P95 | TTFT P95 | Chunks |
|------|---------|---------|------------|-------------|----------|--------|
| In-process mock | 50 | 0 | 549.5 req/s | 19 ms | 12 ms | 200 |
| Local HTTP adapter | 50 | 0 | 61.4 req/s | 98 ms | 28 ms | 150 |

The interesting part is that TTFT stayed relatively low even on the adapter path. Most of the total stream latency increase came after the first chunk because the synthetic upstream paced the stream over multiple writes.

That is a useful reminder: for streaming systems, “time to first token” and “time to full completion” are different operational questions.

## Why The Tenant Limits Had To Change

The seeded local tenant defaults to `60 req/min`, which is a great smoke-test default and a bad benchmark default.

To keep governance from distorting the benchmark, the tenant limits were temporarily raised in MySQL before the benchmark window and restored afterwards. That is not cheating. It is measurement hygiene.

Benchmarking the gateway layer while a rate limiter is intentionally rejecting the run would tell the wrong story.

## The Resource Snapshot Was Also Useful

The 100-request benchmark finished too quickly for one-off process sampling to say much, so a longer observation run was added:

```json
{
  "requests": 2000,
  "concurrency": 100,
  "stream": false,
  "total_duration_ms": 1784,
  "success": 2000,
  "failure": 0,
  "latency_p50_ms": 86,
  "latency_p95_ms": 121,
  "latency_max_ms": 165
}
```

The snapshots taken during that longer run showed:

- gateway RSS around `28 MiB`
- MySQL at about `587.8 MiB`
- Redis at about `9.953 MiB`

That is not a production sizing guide, but it is still operationally useful. It tells you the local dependency footprint is dominated by MySQL, not Redis.

## What These Numbers Do Not Mean

This is the part worth saying plainly.

The adapter-path comparison in these docs is **not** a hosted-provider benchmark. It uses a local synthetic OpenAI-compatible upstream so the run exercises the real adapter code path without mixing in WAN jitter or provider-side variance.

That makes the benchmark honest about gateway overhead, but it does not replace a real hosted-provider latency study.

## Reproducing The Measurements

```bash
# In-process mock path
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 100 -concurrency 10 -json -min-success-rate 1.0
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 50 -concurrency 5 -stream -json -min-success-rate 1.0

# Local HTTP adapter path
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
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 50 -concurrency 5 -stream -json -min-success-rate 1.0
```

## Closing Thought

Performance work gets more useful when it stops trying to look impressive and starts trying to isolate layers.

That is what I like about this benchmark set: one path measures the gateway almost by itself, the other path measures the real HTTP adapter code path, and the methodology document says exactly where the comparison stops being a “real provider” story.

## Related Documentation

- [Non-Streaming Benchmarks](../verification/benchmarks/non-streaming.md)
- [Streaming Benchmarks](../verification/benchmarks/streaming.md)
- [Benchmark Methodology](../verification/benchmarks/methodology.md)
- [Local Development](../local-development.md)
