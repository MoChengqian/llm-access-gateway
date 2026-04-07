# Benchmark Report Template

## Overview

Summarize what was measured and why it matters:

- request type
- provider mode
- expected outcome
- operational question being answered

## Test Environment

Document the environment exactly:

- date
- hostname or machine profile
- deployment mode
- provider configuration
- MySQL and Redis configuration

## Test Configuration

List the commands and flags used.

```bash
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 100 -concurrency 10 -json
```

Explain:

- total requests
- concurrency
- stream mode or non-stream mode
- timeout and thresholds

## Results

Use a table with measured values.

| Metric | Value |
|--------|-------|
| Success | 100 |
| Failure | 0 |
| Latency P50 | 0 ms |
| Latency P95 | 0 ms |

For stream mode also include:

- TTFT P50
- TTFT P95
- TTFT Max
- total stream chunks

## Raw Output

Include the exact JSON or summarized output that produced the table.

```json
{
  "requests": 100,
  "success": 100
}
```

## Analysis

Explain what the numbers mean:

- throughput or latency patterns
- bottlenecks
- anomalies
- limitations of the run

## Reproduction

Provide the same commands again in a minimal copy-paste block so another engineer can repeat the run.

## Related Documentation

- [Benchmark Methodology](../../verification/benchmarks/methodology.md)

Replace this list with the correct related docs.

## Code References

- [`cmd/loadtest/main.go`](../../../cmd/loadtest/main.go)
- [`scripts/gateway-smoke-check.sh`](../../../scripts/gateway-smoke-check.sh)

Replace or extend with the exact implementation files involved.
