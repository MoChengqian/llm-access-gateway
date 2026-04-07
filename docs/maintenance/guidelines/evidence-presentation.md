# Evidence Presentation Guidelines

## Overview

This documentation set is only useful if claims can be checked. Every behavioral claim should be backed by one or more of these evidence types:

- code references
- automated tests
- curl or CLI output
- metrics
- logs
- traces or correlation fields

## Metrics

When presenting metrics:

- name the exact metric
- show the label set when it matters
- explain what the metric proves

Example:

```text
lag_readyz_failures_total 1
```

Explain whether the metric came from:

- a smoke check
- a benchmark run
- a failure drill

## Logs

When quoting logs:

- keep only the fields relevant to the point
- preserve structured fields such as `request_id`, `trace_id`, `backend`, or `status`
- do not invent log keys that are not emitted today

## Traces and Correlation

The repo uses log-based tracing. When documenting trace evidence, refer to:

- `X-Request-Id`
- `X-Trace-Id`
- `trace span finished` logs

Do not describe Jaeger, OTLP, or other tracing backends as if they are configured.

## Benchmarks

Benchmark docs should always include:

- command used
- request count
- concurrency
- stream mode
- result summary
- at least one raw output block

## Failure Drills

Failure drill docs should always include:

- scenario description
- expected behavior before the run
- reproduction commands
- observed HTTP/log/metric evidence
- conclusion

## Handling Blockers

If a verification step could not be completed, state:

- what was attempted
- what blocked it
- what was still verified

Do not silently omit missing evidence.
