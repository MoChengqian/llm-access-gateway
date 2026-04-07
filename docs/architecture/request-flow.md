# Request Flow

This document describes how requests flow through the LLM Access Gateway from client to provider and back. Understanding this flow is essential for debugging, extending, and operating the gateway.

## Overview

Every request to the gateway follows a consistent path through multiple layers:

```text
Client Request
    ↓
API Gateway (Router + Middleware)
    ↓
Authentication
    ↓
Handler
    ↓
Governance Service
    ↓
Chat Service
    ↓
Provider Router
    ↓
Provider Backend
    ↓
Response (streaming or non-streaming)
```

Each layer has a specific responsibility and adds observability context as the request progresses.

## Layer-by-Layer Flow

### 1. API Gateway Layer

**Entry Point:** `internal/api/router.go`

When a request arrives at the gateway, it first passes through the router and a series of middleware functions:

```go
r.Use(chimiddleware.RequestID)      // Generate unique request ID
r.Use(chimiddleware.RealIP)         // Extract real client IP
r.Use(chimiddleware.Recoverer)      // Panic recovery
r.Use(requestIDHeader)              // Add X-Request-Id to response
r.Use(requestTracing(logger))       // Start trace span
r.Use(requestMetrics(metricsRecorder)) // Record metrics
r.Use(requestLogger(logger))        // Log request completion
```

**Request ID Generation:**
- The `chimiddleware.RequestID` middleware generates a unique request ID for every incoming request
- This ID is stored in the request context and can be retrieved with `chimiddleware.GetReqID(ctx)`
- The `requestIDHeader` middleware adds this ID to the response as `X-Request-Id`

**Trace ID Propagation:**
- The `requestTracing` middleware starts the root trace span using `tracing.StartRequestSpan`
- The trace ID is derived from the request ID, ensuring correlation between logs, traces, and responses
- The trace ID is added to the response as `X-Trace-Id`
- All subsequent operations inherit this trace context

**Routing:**
For `POST /v1/chat/completions`, the router applies additional middleware:

```go
r.Post("/v1/chat/completions", chainHandler(
    requireAPIKey(authenticator, chatHandler.CreateCompletion),
    limitRequestBody(maxRequestBodyBytes),
))
```

The request body size is limited before authentication to prevent resource exhaustion attacks.

### 2. Authentication Layer

**Entry Point:** `internal/api/router.go` → `requireAPIKey` → `internal/auth/service.go`

The authentication layer extracts and validates the API key:

**Step 1: Extract Bearer Token**
```go
// From Authorization: Bearer <key>
rawKey, err := bearerToken(authorization)
```

**Step 2: Hash the API Key**
```go
// SHA-256 hash for secure lookup
keyHash := hashAPIKey(rawKey)
```

The gateway never stores raw API keys. All keys are hashed with SHA-256 before storage and lookup.

**Step 3: Lookup API Key in Database**
```go
record, err := s.store.LookupAPIKey(ctx, keyHash)
```

The lookup returns:
- Tenant information (ID, name, RPM limit, TPM limit, token budget)
- API key status (enabled/disabled)
- Tenant status (enabled/disabled)
- API key ID and prefix (for logging)

**Step 4: Validate Status**
```go
if !record.APIKeyEnabled || !record.TenantEnabled {
    return Principal{}, ErrDisabledAPIKey
}
```

**Step 5: Create Principal and Add to Context**
```go
principal := Principal{
    Tenant:       record.Tenant,
    APIKeyID:     record.APIKeyID,
    APIKeyPrefix: record.APIKeyPrefix,
}
ctx = auth.WithPrincipal(ctx, principal)
```

The principal contains all tenant and API key information needed by downstream layers.

**Authentication Errors:**
- `401 Unauthorized` with `WWW-Authenticate: Bearer` header for:
  - Missing API key (`ErrMissingAPIKey`)
  - Invalid API key (`ErrInvalidAPIKey`)
  - Disabled API key or tenant (`ErrDisabledAPIKey`)
- `500 Internal Server Error` for database errors

### 3. Handler Layer

**Entry Point:** `internal/api/handlers/chat.go`

The chat handler is responsible for HTTP concerns:

**Step 1: Parse Request Body**
```go
var req chat.CompletionRequest
if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
    // Return 400 Bad Request
}
```

**Step 2: Check Governance**
```go
tracker, err := h.governanceService.BeginRequest(ctx, governance.RequestMetadata{
    RequestID: chimiddleware.GetReqID(ctx),
    Model:     req.Model,
    Stream:    req.Stream,
    Messages:  req.Messages,
})
```

If governance rejects the request (quota exceeded, rate limit, budget), the handler returns an appropriate error response.

**Step 3: Delegate to Service Layer**
```go
if req.Stream {
    events, err := h.chatService.StreamCompletion(ctx, req)
    // Handle streaming response
} else {
    resp, err := h.chatService.CreateCompletion(ctx, req)
    // Handle non-streaming response
}
```

**Step 4: Track Usage**
For non-streaming:
```go
tracker.CompleteNonStream(ctx, req, resp)
```

For streaming:
```go
for event := range events {
    tracker.ObserveStreamChunk(event.Chunk)
    // Write SSE chunk
}
tracker.CompleteStream(ctx, req)
```

### 4. Governance Layer

**Entry Point:** `internal/service/governance/service.go`

The governance layer enforces tenant quotas and tracks usage:

**Step 1: Extract Principal**
```go
principal, ok := auth.PrincipalFromContext(ctx)
```

**Step 2: Estimate Prompt Tokens**
```go
estimatedPromptTokens := estimatePromptTokens(metadata.Messages)
```

Token estimation is word-based (splits on whitespace) and intentionally simple.

**Step 3: Check Token Budget**
```go
if principal.Tenant.TokenBudget > 0 {
    totalTokensUsed, err := s.store.SumTotalTokens(ctx, principal.Tenant.ID)
    if totalTokensUsed + estimatedPromptTokens > principal.Tenant.TokenBudget {
        return nil, ErrBudgetExceeded
    }
}
```

**Step 4: Check Rate Limits (RPM and TPM)**
```go
if err := s.limiter.Admit(ctx, principal, estimatedPromptTokens, now); err != nil {
    return nil, err // ErrRateLimitExceeded or ErrTokenLimitExceeded
}
```

The limiter uses Redis (with MySQL fallback) to track:
- Requests per minute (RPM)
- Tokens per minute (TPM)

**Step 5: Create Usage Record**
```go
recordID, err := s.store.InsertUsageRecord(ctx, UsageRecord{
    RequestID:    metadata.RequestID,
    TenantID:     principal.Tenant.ID,
    APIKeyID:     principal.APIKeyID,
    Model:        metadata.Model,
    Stream:       metadata.Stream,
    Status:       "started",
    PromptTokens: estimatedPromptTokens,
    TotalTokens:  estimatedPromptTokens,
    CreatedAt:    now,
})
```

**Step 6: Return Request Tracker**
The tracker is used by the handler to update the usage record when the request completes or fails.

**Governance Errors:**
- `429 Too Many Requests` for rate limit exceeded
- `429 Too Many Requests` for token rate limit exceeded
- `429 Too Many Requests` for budget exceeded

### 5. Chat Service Layer

**Entry Point:** `internal/service/chat/service.go`

The chat service validates the request and normalizes provider responses:

**Step 1: Start Trace Span**
```go
ctx, span := tracing.StartSpan(ctx, "chat.service.create_completion",
    zap.String("model", req.Model),
    zap.String("stream", strconv.FormatBool(req.Stream)),
)
defer span.End(err)
```

**Step 2: Validate Request**
```go
if len(req.Messages) == 0 {
    return CompletionResponse{}, ErrInvalidRequest
}
```

**Step 3: Apply Default Model**
```go
model := req.Model
if model == "" {
    model = s.defaultModel
}
```

**Step 4: Convert to Provider Format**
```go
providerReq := provider.ChatCompletionRequest{
    Model:    model,
    Messages: toProviderMessages(req.Messages),
    Stream:   req.Stream,
}
```

**Step 5: Call Provider**
```go
providerResp, err := s.provider.CreateChatCompletion(ctx, providerReq)
```

**Step 6: Convert Provider Response to Service Format**
The service layer normalizes provider responses into a consistent format for the handler.

### 6. Provider Router Layer

**Entry Point:** `internal/provider/router/chat.go`

The provider router handles backend selection, fallback, and health tracking:

**Step 1: Start Trace Span**
```go
ctx, span := tracing.StartSpan(ctx, "provider.router.create",
    zap.String("model", req.Model),
    zap.Int("configured_backends", len(p.backends)),
)
defer span.End(err)
```

**Step 2: Select Candidate Backends**
```go
candidates, skipped := p.candidates()
```

Backends in cooldown (marked unhealthy) are skipped. If all backends are unhealthy, they are all tried anyway.

**Step 3: Try Each Backend in Order**
```go
for index, backend := range candidates {
    attemptCtx, attemptSpan := tracing.StartSpan(ctx, "provider.backend.create",
        zap.String("backend", backend.Name),
        zap.Int("attempt", index+1),
    )
    resp, err := backend.Provider.CreateChatCompletion(attemptCtx, req)
    attemptSpan.End(err)
    
    if err == nil {
        p.markSuccess(backend.Name)
        return resp, nil
    }
    
    p.markFailure(backend.Name)
}
```

**Passive Health Tracking:**
- Each failure increments the backend's consecutive failure count
- When failures reach the threshold (default: 1), the backend enters cooldown
- During cooldown, the backend is skipped for new requests
- Success resets the failure count and clears cooldown

**Fallback Behavior:**

For non-streaming requests:
- All backends are tried in order until one succeeds
- Failures are tracked and backends enter cooldown

For streaming requests:
- Fallback only happens before the first chunk is sent
- Once the first chunk is successfully received, no fallback occurs
- If the stream fails after the first chunk, the error is propagated to the client

**Step 4: Await First Stream Chunk (Streaming Only)**
```go
firstEvent, err := p.awaitFirstStreamEvent(attemptCtx, events)
if err != nil {
    // Try next backend
}
```

This is the critical fallback boundary for streaming requests.

**Step 5: Wrap Stream and Forward Events**
```go
return p.wrapStream(ctx, events, firstEvent, span, attemptSpan, backend.Name, attempt), nil
```

The wrapper:
- Forwards the first event immediately
- Forwards subsequent events as they arrive
- Tracks stream interruptions and marks failures
- Ends trace spans when the stream completes

### 7. Provider Backend Layer

**Entry Point:** `internal/provider/openai/chat.go` or `internal/provider/mock/chat.go`

The provider backend makes the actual HTTP request to the upstream provider:

**For Non-Streaming:**
```go
resp, err := http.Post(url, "application/json", body)
// Parse response
// Return provider.ChatCompletionResponse
```

**For Streaming:**
```go
resp, err := http.Post(url, "application/json", body)
// Read SSE stream
// Parse each chunk
// Send events to channel
```

The provider backend is responsible for:
- Making HTTP requests to upstream providers
- Parsing provider-specific response formats
- Converting to the gateway's unified provider format
- Handling provider-specific errors

## Response Path

### Non-Streaming Response

**Provider → Router → Service → Handler → Client**

1. Provider backend returns `provider.ChatCompletionResponse`
2. Router marks success and returns response
3. Service converts to `chat.CompletionResponse`
4. Handler updates usage tracker and writes JSON response
5. Client receives complete response

### Streaming Response

**Provider → Router → Service → Handler → Client**

1. Provider backend sends events to channel
2. Router wraps stream and forwards events
3. Service converts events to `chat.CompletionEvent`
4. Handler writes SSE chunks as they arrive
5. Client receives stream in real-time

**SSE Format:**
```text
data: {"id":"...","object":"chat.completion.chunk","created":...,"model":"...","choices":[...]}

data: {"id":"...","object":"chat.completion.chunk","created":...,"model":"...","choices":[...]}

data: [DONE]
```

Each chunk is prefixed with `data: ` and followed by two newlines. The stream ends with `data: [DONE]\n\n`.

## Request ID and Trace ID Propagation

### Request ID Flow

1. **Generated:** `chimiddleware.RequestID` middleware
2. **Stored:** Request context (`chimiddleware.GetReqID(ctx)`)
3. **Propagated:** All trace spans, log entries, usage records
4. **Returned:** `X-Request-Id` response header

### Trace ID Flow

1. **Generated:** `tracing.StartRequestSpan` (derived from request ID)
2. **Stored:** Trace context (`tracing.TraceIDFromContext(ctx)`)
3. **Propagated:** All child spans, log entries
4. **Returned:** `X-Trace-Id` response header

### Span Hierarchy

```text
http.request (root span)
  ├─ chat.service.create_completion
  │   └─ provider.router.create
  │       ├─ provider.backend.create (attempt 1)
  │       └─ provider.backend.create (attempt 2, if fallback)
```

Each span includes:
- `trace_id`: Links all spans in the request
- `span_id`: Unique identifier for this span
- `parent_span_id`: Links to parent span
- `request_id`: Original request ID
- `span_name`: Operation name
- `status`: "ok" or "error"
- `duration`: Time spent in this span

### Log Correlation

Every log entry includes:
- `request_id`: Original request ID
- `trace_id`: Current trace ID
- `span_id`: Current span ID
- `tenant_name`: Tenant name (after auth)
- `tenant_id`: Tenant ID (after auth)
- `api_key_id`: API key ID (after auth)
- `api_key_prefix`: API key prefix (after auth)

This allows correlating logs across the entire request lifecycle.

## Error Handling

Errors at each layer result in different HTTP status codes:

| Layer | Error | Status Code |
|-------|-------|-------------|
| Router | Request body too large | 413 Payload Too Large |
| Auth | Missing API key | 401 Unauthorized |
| Auth | Invalid API key | 401 Unauthorized |
| Auth | Disabled API key | 401 Unauthorized |
| Auth | Database error | 500 Internal Server Error |
| Handler | Invalid JSON | 400 Bad Request |
| Governance | Rate limit exceeded | 429 Too Many Requests |
| Governance | Token limit exceeded | 429 Too Many Requests |
| Governance | Budget exceeded | 429 Too Many Requests |
| Service | Missing messages | 400 Bad Request |
| Provider | All backends failed | 502 Bad Gateway |
| Provider | Timeout | 504 Gateway Timeout |

## Performance Considerations

### Request Latency Breakdown

For a typical non-streaming request:

1. **Middleware:** ~1ms (request ID, tracing, metrics)
2. **Authentication:** ~5-10ms (database lookup)
3. **Governance:** ~5-15ms (rate limit check, usage record creation)
4. **Service:** <1ms (validation, format conversion)
5. **Provider Router:** <1ms (backend selection)
6. **Provider Backend:** 500-2000ms (upstream provider latency)
7. **Response:** ~1ms (JSON serialization)

**Total:** ~510-2030ms (dominated by upstream provider)

### Streaming Latency

For streaming requests, the critical metric is Time To First Token (TTFT):

1. **Middleware → Provider Backend:** ~10-30ms
2. **Upstream TTFT:** 200-500ms (provider-dependent)
3. **First Chunk to Client:** ~1ms

**Total TTFT:** ~210-530ms

After the first chunk, subsequent chunks are forwarded with minimal overhead (<1ms per chunk).

## Observability

### Metrics

The gateway records metrics at multiple points:

- **HTTP requests:** Method, path, status, duration
- **Readiness failures:** When `/readyz` returns non-200
- **Governance rejections:** Reason (rate limit, token limit, budget)
- **Stream requests:** TTFT duration
- **Stream chunks:** Count per request

### Traces

Trace spans are created for:

- HTTP request (root span)
- Chat service operations
- Provider router operations
- Provider backend attempts

Each span includes operation-specific fields and timing information.

### Logs

Structured logs are emitted for:

- HTTP request completion (with full context)
- Trace span completion (with timing and status)
- Provider events (success, failure, fallback, recovery)

All logs include request ID, trace ID, span ID, and tenant information for correlation.

## Example: Complete Request Flow

Here's a concrete example of a non-streaming request:

**Request:**
```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer lag-local-dev-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

**Flow:**

1. **Router:** Generate request ID `abc123`, start trace `abc123`
2. **Auth:** Hash key, lookup in database, find tenant `local-dev`
3. **Handler:** Parse JSON, extract model and messages
4. **Governance:** Check RPM (10/100), check TPM (5/1000), check budget (100/10000), create usage record
5. **Service:** Validate messages, apply default model, convert to provider format
6. **Router:** Select primary backend `openai-primary`, start attempt 1
7. **Backend:** POST to OpenAI API, receive response in 800ms
8. **Router:** Mark success, return response
9. **Service:** Convert to service format
10. **Handler:** Update usage record (prompt: 5, completion: 10, total: 15), write JSON response
11. **Client:** Receive response with `X-Request-Id: abc123` and `X-Trace-Id: abc123`

**Logs:**
```json
{"level":"info","msg":"trace span finished","trace_id":"abc123","span_id":"1","span_name":"http.request","status":"ok","duration":"850ms"}
{"level":"info","msg":"trace span finished","trace_id":"abc123","span_id":"2","parent_span_id":"1","span_name":"chat.service.create_completion","status":"ok","duration":"820ms"}
{"level":"info","msg":"trace span finished","trace_id":"abc123","span_id":"3","parent_span_id":"2","span_name":"provider.router.create","status":"ok","duration":"810ms"}
{"level":"info","msg":"trace span finished","trace_id":"abc123","span_id":"4","parent_span_id":"3","span_name":"provider.backend.create","backend":"openai-primary","attempt":1,"status":"ok","duration":"800ms"}
{"level":"info","msg":"http request completed","request_id":"abc123","trace_id":"abc123","method":"POST","path":"/v1/chat/completions","status":200,"duration":"850ms","tenant_name":"local-dev"}
```

## Related Documentation

- [Architecture Overview](overview.md) - High-level system architecture
- [Provider Adapters](provider-adapters.md) - Provider abstraction design
- [Streaming Proxy](streaming-proxy.md) - SSE streaming implementation
- [Governance](governance.md) - Auth, tenant, and quota model
- [Routing and Resilience](routing-resilience.md) - Routing, retry, and fallback
- [Observability](observability.md) - Metrics, tracing, and logs
