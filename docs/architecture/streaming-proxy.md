# Streaming Proxy Architecture

## Overview

The LLM Access Gateway implements a Server-Sent Events (SSE) streaming proxy that forwards streaming chat completions from upstream LLM providers to clients. The streaming proxy is designed to minimize Time To First Token (TTFT), handle failures gracefully, and maintain observability throughout the request lifecycle.

This document explains the streaming proxy implementation, flush behavior, TTFT measurement, fallback constraints, and client disconnect handling.

## SSE Streaming Flow

The streaming proxy operates across three layers:

1. **HTTP Handler Layer** (`internal/api/handlers/chat.go`): Manages HTTP response streaming, SSE formatting, and client connection
2. **Service Layer** (`internal/service/chat/service.go`): Transforms provider events into service-level completion events
3. **Provider Router Layer** (`internal/provider/router/chat.go`): Handles provider selection, fallback, and health tracking

### Request Flow

```
Client Request (stream=true)
    ↓
HTTP Handler: Validate flusher support
    ↓
Governance: Check quotas and begin request tracking
    ↓
Service Layer: Prepare provider request
    ↓
Provider Router: Select healthy backend
    ↓
Provider Adapter: Open upstream SSE stream
    ↓
Provider Router: Await first chunk (fallback window)
    ↓
HTTP Handler: Write SSE headers and first chunk
    ↓
HTTP Handler: Flush immediately (TTFT measurement)
    ↓
[Loop] Forward remaining chunks with immediate flush
    ↓
HTTP Handler: Write "data: [DONE]\n\n" marker
    ↓
Governance: Complete request tracking with usage data
```

## Flush Behavior and TTFT Measurement

### Immediate Flush Strategy

The gateway flushes each SSE chunk immediately after writing it to the HTTP response. This minimizes latency and ensures clients receive tokens as soon as they arrive from the upstream provider.

**Implementation** (`internal/api/handlers/chat.go`):

```go
for event := range events {
    // ... write chunk as SSE data ...
    
    if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
        // handle error
    }
    
    if h.metrics != nil {
        if !firstChunkWritten {
            h.metrics.RecordStreamRequest(time.Since(startedAt))
            firstChunkWritten = true
        }
        h.metrics.RecordStreamChunk()
    }
    
    flusher.Flush()  // Immediate flush after each chunk
}
```

### TTFT Measurement

Time To First Token (TTFT) is measured from the moment the handler begins processing the streaming request until the first chunk is written to the client.

**Measurement Points**:
- **Start**: `startedAt := time.Now()` at the beginning of `streamCompletion()`
- **End**: When the first chunk is written and flushed
- **Recording**: `h.metrics.RecordStreamRequest(time.Since(startedAt))`

This measurement captures the complete latency including:
- Governance checks (auth, quota validation)
- Provider selection and health checks
- Upstream provider TTFT
- Network latency to upstream provider
- First chunk serialization and transmission

## Fallback Constraint: Only Before First Chunk

The streaming proxy enforces a critical constraint: **provider fallback is only possible before the first chunk is sent to the client**.

### Why This Constraint Exists

Once the HTTP handler writes the first SSE chunk and flushes it to the client:
1. HTTP status code (200 OK) has been sent
2. SSE headers (`Content-Type: text/event-stream`) have been sent
3. Client has begun receiving the response stream
4. The HTTP response cannot be "taken back" or replaced with an error

If the gateway attempted fallback after sending the first chunk, the client would receive:
- Partial response from Provider A
- Followed by a new response from Provider B (with different ID, different content)
- This would violate the SSE protocol and produce corrupted output

### Implementation

**Provider Router** (`internal/provider/router/chat.go`):

The router waits for the first chunk from the upstream provider before returning the stream to the caller. This creates a fallback window where failures can trigger provider fallback.

```go
func (p *Provider) StreamChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (<-chan provider.ChatCompletionStreamEvent, error) {
    // ... select candidates ...
    
    for index, backend := range candidates {
        events, err := backend.Provider.StreamChatCompletion(attemptCtx, req)
        if err != nil {
            // Fallback: stream failed to open
            continue
        }
        
        // CRITICAL: Await first chunk before returning
        firstEvent, err := p.awaitFirstStreamEvent(attemptCtx, events)
        if err != nil {
            // Fallback: first chunk failed to arrive
            continue
        }
        
        // First chunk received successfully - no more fallback possible
        return p.wrapStream(ctx, events, firstEvent, ...), nil
    }
    
    return nil, lastErr
}
```

**Awaiting First Event**:

```go
func (p *Provider) awaitFirstStreamEvent(ctx context.Context, events <-chan provider.ChatCompletionStreamEvent) (provider.ChatCompletionStreamEvent, error) {
    select {
    case <-ctx.Done():
        return provider.ChatCompletionStreamEvent{}, ctx.Err()
    case event, ok := <-events:
        if !ok {
            return provider.ChatCompletionStreamEvent{}, errors.New("upstream stream closed before first chunk")
        }
        if event.Err != nil {
            return provider.ChatCompletionStreamEvent{}, event.Err
        }
        return event, nil
    }
}
```

### After First Chunk: Stream Interruption

If a failure occurs after the first chunk has been sent, the gateway:
1. Marks the provider as unhealthy (increments consecutive failures)
2. Forwards the error event to the client
3. Closes the stream
4. Records observability events (`provider_stream_interrupted`)

**No fallback occurs** - the client receives a partial response and an error.

**Implementation** (`internal/provider/router/chat.go`):

```go
func (p *Provider) forwardStreamEvent(...) bool {
    if event.Err != nil {
        *traceErr = event.Err
        failures, unhealthyUntil := p.markFailure(backend)
        p.observe(Event{
            Type:                "provider_stream_interrupted",
            Operation:           "stream",
            Backend:             backend,
            ConsecutiveFailures: failures,
            UnhealthyUntil:      unhealthyUntil,
            Error:               event.Err.Error(),
        })
        // Forward error to client
        select {
        case <-ctx.Done():
        case wrapped <- event:
        }
        return false  // Stop forwarding
    }
    // ... forward successful chunk ...
}
```

## Client Disconnect Handling

The gateway detects client disconnects through context cancellation and stops processing immediately.

### Detection Mechanism

The HTTP handler's context is cancelled when:
- Client closes the connection
- Client TCP connection is reset
- Network failure between gateway and client

### Handler Response

**HTTP Handler** (`internal/api/handlers/chat.go`):

The streaming loop checks for context cancellation before forwarding each event:

```go
for event := range events {
    if event.Err != nil {
        // ... handle error ...
    }
    
    // ... prepare chunk ...
    
    if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
        // Write failed - likely client disconnect
        _ = tracker.Fail(ctx)
        return
    }
    
    flusher.Flush()
}
```

### Provider Router Response

**Provider Router** (`internal/provider/router/chat.go`):

The router's stream wrapper checks context cancellation in two places:

1. **Before forwarding each chunk**:
```go
func (p *Provider) forwardStreamEvent(...) bool {
    // ... handle errors ...
    
    select {
    case <-ctx.Done():
        *traceErr = ctx.Err()
        return false  // Stop forwarding
    case wrapped <- event:
        *chunkCount = *chunkCount + 1
        return true
    }
}
```

2. **In the main forwarding loop**:
```go
func (p *Provider) wrapStream(...) <-chan provider.ChatCompletionStreamEvent {
    wrapped := make(chan provider.ChatCompletionStreamEvent)
    
    go func() {
        defer close(wrapped)
        // ... forward first event ...
        
        for {
            select {
            case <-ctx.Done():
                traceErr = ctx.Err()
                return  // Stop reading from upstream
            case event, ok := <-events:
                if !ok {
                    return
                }
                if !p.forwardStreamEvent(...) {
                    return
                }
            }
        }
    }()
    
    return wrapped
}
```

### Upstream Provider Cleanup

When the context is cancelled, the provider adapter's goroutine (reading from the upstream SSE stream) will:
1. Detect context cancellation
2. Stop reading from the upstream connection
3. Close the response body
4. Close the event channel

**Provider Adapter** (`internal/provider/openai/chat.go`):

```go
func (p Provider) StreamChatCompletion(...) (<-chan provider.ChatCompletionStreamEvent, error) {
    // ... open stream ...
    
    events := make(chan provider.ChatCompletionStreamEvent)
    go func() {
        defer close(events)
        defer func() {
            _ = resp.Body.Close()  // Cleanup upstream connection
        }()
        
        reader := bufio.NewReader(resp.Body)
        for {
            line, err := reader.ReadString('\n')
            // ... parse SSE line ...
            
            select {
            case <-ctx.Done():
                publishStreamError(ctx, events, ctx.Err())
                return  // Stop reading from upstream
            case events <- provider.ChatCompletionStreamEvent{...}:
            }
        }
    }()
    
    return events, nil
}
```

## SSE Format Details

### Chunk Format

Each streaming chunk is formatted as an SSE event:

```
data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

```

**Format Rules**:
- Each chunk starts with `data: `
- Followed by JSON-encoded completion chunk
- Followed by two newlines (`\n\n`)
- No blank lines between chunks

### Stream Termination

The stream ends with the `[DONE]` marker:

```
data: [DONE]

```

**Implementation** (`internal/api/handlers/chat.go`):

```go
if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
    _ = tracker.Fail(ctx)
    return
}
flusher.Flush()
```

### Headers

The HTTP handler sets SSE-specific headers before writing the first chunk:

```go
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")
w.WriteHeader(http.StatusOK)
```

## Observability

### Metrics

The streaming proxy records:
- **TTFT**: Time from request start to first chunk (`RecordStreamRequest`)
- **Chunk Count**: Number of chunks forwarded (`RecordStreamChunk`)

### Tracing

Spans are created at each layer:
- `chat.handler.stream_completion`: HTTP handler span
- `chat.service.stream_completion`: Service layer span
- `provider.router.stream`: Router span
- `provider.backend.stream`: Per-backend attempt span

Each span includes:
- Model name
- Backend name (router/backend spans)
- Attempt number (backend spans)
- Chunk count (recorded at span end)
- Error information (if applicable)

### Events

The provider router emits events for observability:
- `provider_request_succeeded`: First chunk received successfully
- `provider_request_failed`: Failed before first chunk (triggers fallback)
- `provider_stream_interrupted`: Error after first chunk (no fallback)
- `provider_fallback_succeeded`: Fallback succeeded after initial failure
- `provider_recovered`: Previously unhealthy provider succeeded

## Error Scenarios

### Before First Chunk

**Scenario**: Upstream provider fails to return first chunk within timeout

**Behavior**:
1. Router detects failure in `awaitFirstStreamEvent`
2. Router marks provider as unhealthy
3. Router attempts next candidate provider
4. If all providers fail, return error to handler
5. Handler writes JSON error response (no SSE headers sent yet)

**Client Experience**: Receives HTTP error response (500 or 503)

### After First Chunk

**Scenario**: Upstream provider stream fails mid-response

**Behavior**:
1. Router detects error event from provider
2. Router marks provider as unhealthy
3. Router forwards error event to handler
4. Handler stops processing (cannot send error response - headers already sent)
5. Client connection closes

**Client Experience**: Receives partial SSE stream, then connection closes

### Client Disconnect

**Scenario**: Client closes connection while stream is active

**Behavior**:
1. Handler context is cancelled
2. Handler stops reading from service events
3. Service layer detects context cancellation, stops reading from router
4. Router detects context cancellation, stops reading from provider
5. Provider adapter closes upstream connection

**Upstream Impact**: Upstream connection is closed promptly, preventing wasted resources

## Design Rationale

### Why Immediate Flush?

Immediate flushing after each chunk minimizes latency and provides the best user experience for streaming responses. The overhead of multiple flush operations is negligible compared to the latency improvement.

### Why Wait for First Chunk?

Waiting for the first chunk before returning the stream to the handler creates a fallback window. This allows the router to try alternative providers if the initial provider fails before sending any data, preventing partial responses from reaching the client.

### Why No Fallback After First Chunk?

HTTP is a request-response protocol. Once the response has started (status code and headers sent), it cannot be replaced or retried. Attempting fallback after the first chunk would result in corrupted responses with mixed content from multiple providers.

### Why Context-Based Disconnect Detection?

Go's context cancellation provides a clean, idiomatic way to propagate cancellation signals through the entire request pipeline. When the client disconnects, the context is cancelled, and all goroutines in the pipeline detect this and stop processing.

## Related Documentation

- [SSE Streaming Format](../api/streaming.md) - Client-facing SSE format documentation
- [Request Flow](request-flow.md) - Complete request flow through all layers
- [Routing and Resilience](routing-resilience.md) - Provider selection and fallback logic
- [Observability](observability.md) - Metrics, tracing, and logging details

## Code References

- HTTP Handler: `internal/api/handlers/chat.go` - `streamCompletion()` function
- Service Layer: `internal/service/chat/service.go` - `StreamCompletion()` function
- Provider Router: `internal/provider/router/chat.go` - `StreamChatCompletion()`, `awaitFirstStreamEvent()`, `wrapStream()`, `forwardStreamEvent()`
- Provider Adapter: `internal/provider/openai/chat.go` - `StreamChatCompletion()` function
