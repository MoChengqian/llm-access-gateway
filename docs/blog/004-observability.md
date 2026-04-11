# 004: Observability That Starts With Correlation, Not Dashboards

## Overview

A gateway is only as operable as its failure stories.

When a request fails somewhere between auth, governance, routing, and an upstream provider, the first question is not “Do we have a dashboard?” It is “Can we correlate what just happened?”

That is why this repository starts observability with request IDs, trace IDs,
structured logs, and a small metrics surface before adding exporter or dashboard
assets.

## The First Win: Every Request Gets an Identity

The router assigns a request ID and exposes it as `X-Request-Id`.

Tracing then derives a root trace ID and exposes it as `X-Trace-Id`.

From that point on, the same identifiers move through:

- middleware
- handlers
- services
- provider router
- provider backend spans
- final access logs

That single design choice changes the debugging experience completely. Instead of guessing which log lines belong together, you can walk one request from the edge to the provider attempt that failed.

## Tracing: Logs First, OTLP When Needed

One thing I like about the current design is its honesty.

By default, the gateway does not require a tracing backend to be useful. It emits
span-completion logs with:

- `trace_id`
- `span_id`
- `parent_span_id`
- `request_id`
- `span_name`
- `status`
- `duration`

That is enough to reconstruct a request path when you need to answer questions like:

- did the request fail in auth, governance, or provider routing?
- which backend attempt failed?
- how long did the upstream call take?
- was the stream interrupted before or after output started?

Stage 6 adds the external path without removing that lightweight workflow:

- `internal/obs/tracing/tracing.go` still logs every span
- `internal/obs/oteltracing/oteltracing.go` configures optional OTLP/HTTP export
- `APP_OBSERVABILITY_OTLP_TRACES_ENDPOINT` enables the exporter
- exported spans keep `lag.trace_id`, `lag.span_id`, and `lag.request_id`

The important boundary is that this repository now provides the exporter path,
not a managed Jaeger, Tempo, or collector installation.

## Metrics That Match the Gateway’s Real Questions

The `/metrics` endpoint is deliberately small and operationally focused.

It answers the questions that matter for this service:

- how many HTTP requests are we serving?
- how long are they taking?
- are providers failing or recovering?
- are readiness failures increasing?
- are governance rejections happening?
- what is the stream TTFT profile?
- which backend is currently healthy right now?

That is a better starting point than a huge metrics surface with no connection to actual operating decisions.

## Why Structured Logs Matter Here

The gateway sits in the middle of many concerns:

- auth
- quota enforcement
- provider fallback
- stream forwarding
- health transitions

If those events are not logged with consistent keys, you lose the plot fast.

The current implementation uses zap structured logs for:

- request completion
- span completion
- provider events

That means routing events such as fallback, skipped unhealthy backends, probe failures, and recoveries can be correlated with the same request IDs and trace IDs the client sees.

The metrics surface now complements that with current-state gauges for backend health, consecutive failures, cooldown remaining time, and overall provider readiness. That matters because operators no longer need to choose between:

- counters that explain the past
- `/debug/providers` JSON that explains the present

They can scrape both stories from the same metrics endpoint.

## A Practical Debugging Loop

For this kind of service, the most useful debugging loop is simple:

1. capture `X-Request-Id` and `X-Trace-Id`
2. search logs for that request
3. inspect the provider event stream
4. inspect `/debug/providers`
5. inspect `/metrics` for aggregate signs of the same problem

That workflow is more valuable than a sophisticated observability story that nobody can actually use under pressure.

## What This Design Does Not Do Yet

The boundaries are worth stating clearly:

- metrics are process-local and reset on restart
- trace export is optional; tracing storage remains environment-owned
- the registry exposes counts and sums, not histogram buckets
- the Grafana dashboard is committed as an importable asset, not as a managed service

Those are not flaws hidden under the rug. They are the current shape of the system. Good observability docs should say that plainly.

## Related Documentation

- [Observability Design](../architecture/observability.md)
- [Request Flow](../architecture/request-flow.md)
- [Routing and Resilience](../architecture/routing-resilience.md)
- [Local Development](../local-development.md)
