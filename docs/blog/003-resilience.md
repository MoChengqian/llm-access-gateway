# 003: Resilience Starts With Honest Failure Boundaries

## Overview

Resilience in an LLM gateway is not the same thing as “just retry it.”

This repository is a good example of why. The gateway has to make different decisions depending on where the failure happens:

- before any response bytes are sent
- after a stream has already started
- inside provider adapter retries
- at the router level when a backend is no longer trustworthy

The code handles those cases differently on purpose.

## The Router Is Simple On Purpose

The current router is not doing fancy weighted scheduling. It is doing something more important for a small gateway: deterministic ordered failover.

The backend order is effectively:

1. primary
2. secondary

That matters because it makes failures easy to reason about. When the primary fails, you know exactly what the router will try next.

## Retry And Fallback Are Different Layers

One of the easiest mistakes in gateway design is to blur adapter retry and router fallback into the same idea. They are not the same thing here.

The OpenAI-compatible adapter retries network timeouts and retryable HTTP statuses inside `internal/provider/openai/chat.go`.

The router in `internal/provider/router/chat.go` then decides whether to:

- mark the backend unhealthy
- move to the next backend
- or stop because the stream already started

That layering showed up clearly in the drills.

## What The Failure Drills Actually Showed

### Timeout

With a synthetic upstream that slept for `2.5s` and an adapter timeout of `1s`, the request still returned `200` through the secondary backend in about `1.017s`.

That run also exposed an important detail: there was no second upstream HTTP attempt. The first timeout consumed the adapter's `1s` request context, so the router moved directly to fallback.

### Upstream 500

With a synthetic upstream that always returned `500`, the request still came back `200` in about `214ms`.

This time the upstream logged two `500` responses for the same gateway request. That proved the adapter retried once before the router marked the backend unhealthy and failed over to the mock secondary provider.

### Streaming Failure Before The First Chunk

When the synthetic upstream returned a streaming failure before the first chunk, the client still received a complete mock SSE response with `data: [DONE]`.

That is the best-case streaming failure mode, because the router still owns the response before the first bytes are committed to the client.

### Streaming Failure After The First Chunk

When the synthetic upstream emitted one chunk and then closed the stream, the client saw:

- `HTTP/1.1 200 OK`
- one partial SSE chunk
- no final `data: [DONE]`

There was no fallback, and that is correct. Once the response starts, the gateway cannot splice in a different backend without corrupting the stream.

## Passive Health Tracking Is Enough To Be Useful

The gateway does not rely on a large external health system. Instead it tracks provider health passively:

- request failures increment consecutive failure counters
- failure threshold crossing pushes the backend into cooldown
- probe success clears the unhealthy state

In practice that was enough to make the drills visible and explainable:

- `provider_request_failed`
- `provider_fallback_succeeded`
- `provider_stream_interrupted`
- `provider_recovered`

The `/debug/providers` endpoint made the state transition obvious, and `/readyz` stayed green whenever the secondary backend was still healthy.

## Why Readiness Stays Green

This is one of the design choices I like most in the current implementation.

Readiness does not mean “the primary provider is healthy.” It means the gateway can still serve requests.

That distinction showed up in every failure drill:

- primary entered cooldown
- secondary stayed healthy
- `/readyz` still returned `200`

That is the right contract for traffic management. You should drain the gateway only when it has no usable backend path left.

## Reproducing The Drills

The repo now includes a small synthetic upstream helper for the HTTP adapter path:

```bash
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode error500
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode timeout --delay-seconds 2.5
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode stream_prechunk_fail
python3 ./scripts/synthetic-openai-upstream.py --port 18081 --mode stream_partial
```

Pair that with:

```bash
./scripts/provider-fallback-drill.sh create-fail
./scripts/provider-fallback-drill.sh stream-fail
```

and the gateway-side provider env vars documented in the failure drill reports.

## Closing Thought

The strongest thing about this resilience story is not that it hides failure well. It is that it makes failure legible.

You can watch the router mark a backend unhealthy, see cooldown kick in, confirm fallback happened, and understand exactly why a stream did or did not switch providers. That kind of honesty scales better than a lot of “smart” routing systems.

## Related Documentation

- [Routing and Resilience](../architecture/routing-resilience.md)
- [Provider Timeout Drill](../verification/failure-drills/provider-timeout.md)
- [Provider Error Drill](../verification/failure-drills/provider-errors.md)
- [Streaming Failure Drill](../verification/failure-drills/streaming-failures.md)
