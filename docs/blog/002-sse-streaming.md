# 002: Building an SSE Streaming Proxy Without Lying to the Client

## Overview

Streaming looks simple from the outside: open an HTTP connection, forward chunks, and end with `[DONE]`.

The hard part is failure behavior.

Once a client has received the first stream chunk, the response has already started. At that point, the gateway cannot safely pretend nothing happened and switch to another provider. The implementation in this repository is built around that constraint.

## The Important Boundary

For non-streaming requests, fallback is straightforward:

1. try the primary backend
2. if it fails, try the secondary backend
3. return whichever succeeds first

Streaming changes the rules.

The gateway only allows fallback before the first chunk arrives from upstream. The router explicitly waits for that first event before it commits to a backend.

That one design choice protects response integrity:

- clients do not receive a partial response from provider A and then a second response from provider B
- the gateway never emits a fake `[DONE]` after a mid-stream interruption
- failure handling stays honest about what the client actually received

## The Three Layers of Streaming

The current implementation splits streaming responsibilities across three layers.

### HTTP handler

The chat handler:

- verifies that the response writer supports flushing
- sets `Content-Type: text/event-stream`
- writes each chunk as `data: ...`
- flushes immediately
- writes `data: [DONE]` only when the stream completes normally

This is also where TTFT is measured for the outgoing client response.

### Chat service

The chat service converts provider events into service-level completion events and preserves the request context for tracing and cancellation.

### Provider router

The router owns:

- backend selection
- pre-stream fallback
- health tracking
- interruption handling after the stream has started

This separation keeps the HTTP layer focused on protocol behavior while the router handles resilience logic.

## Why Immediate Flush Matters

A streaming proxy that buffers too much stops feeling like a stream.

In this repository, each chunk is flushed as soon as it is written. That gives the client the shortest path from upstream token arrival to downstream delivery and makes TTFT meaningful as a metric.

Because the first flush is also the point of no return, the implementation naturally couples:

- first-chunk acceptance
- TTFT measurement
- end of the fallback window

That coupling is not accidental. It reflects the actual boundary of the protocol.

## What Happens on Failure

There are two very different failure classes.

### Failure before the first chunk

Examples:

- stream open failed
- upstream closed before sending a chunk
- first event carried an error

In those cases, the router is still free to try another backend.

### Failure after the first chunk

Examples:

- upstream breaks mid-stream
- context cancellation during forwarding
- downstream write fails

At that point, the gateway forwards the error condition and closes the stream. No fallback occurs. That is the right behavior because the client has already observed the response begin.

## A Good Streaming Design Is Mostly Constraint Management

The interesting thing about this implementation is that it does not pretend streaming is the same as ordinary request/response I/O.

Instead, it treats the protocol boundary as a first-class design input:

- before first chunk: recover if possible
- after first chunk: preserve integrity and surface interruption honestly

That is a practical pattern for any LLM gateway, no matter which provider sits behind it.

## Reproduce It Locally

Once the local environment is up, you can verify the behavior with:

```bash
curl -i -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}],"stream":true}'
```

And for load-oriented checks:

```bash
go run ./cmd/loadtest -auth-key lag-local-dev-key -requests 50 -concurrency 5 -stream
```

For failure behavior:

```bash
./scripts/provider-fallback-drill.sh stream-fail
```

## Related Documentation

- [SSE Streaming Format](../api/streaming.md)
- [Streaming Proxy Architecture](../architecture/streaming-proxy.md)
- [Routing and Resilience](../architecture/routing-resilience.md)
- [Request Flow](../architecture/request-flow.md)
