# Provider Adapter Design

## Overview

The LLM Access Gateway uses a provider adapter pattern to abstract different LLM providers (OpenAI-compatible APIs, Anthropic's Messages API, Ollama, local mocks, and future adapters) behind a unified interface. This design allows the gateway to support multiple providers without coupling the core routing, governance, and API layers to provider-specific implementations.

The adapter pattern provides three key benefits:

1. **Unified Interface**: All providers implement the same Go interfaces, allowing the router to treat them uniformly
2. **Error Normalization**: Provider-specific errors are translated into consistent error semantics
3. **Request/Response Translation**: Provider-specific formats are normalized to a common internal representation

## Provider Abstraction

### Core Interfaces

The gateway defines two core provider interfaces in [`internal/provider/provider.go`](../../internal/provider/provider.go):

```go
type ChatCompletionProvider interface {
    CreateChatCompletion(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error)
    StreamChatCompletion(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionStreamEvent, error)
}

type ModelProvider interface {
    ListModels(ctx context.Context) ([]Model, error)
}
```

**Why this abstraction exists:**

- **Routing Independence**: The router in [`internal/provider/router/chat.go`](../../internal/provider/router/chat.go) can route requests to any provider without knowing implementation details
- **Fallback Simplicity**: When a provider fails, the router can seamlessly try the next provider using the same interface
- **Testing**: Mock providers can be injected for testing without changing routing logic
- **Provider Diversity**: New providers can be added by implementing these interfaces without modifying existing code

### Unified Request/Response Format

All providers translate their specific formats to a common internal representation:

**Request Format** ([`internal/provider/provider.go`](../../internal/provider/provider.go)):
```go
type ChatCompletionRequest struct {
    Model    string        `json:"model"`
    Messages []ChatMessage `json:"messages"`
    Stream   bool          `json:"stream"`
}

type ChatMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}
```

**Response Format** (Non-Streaming):
```go
type ChatCompletionResponse struct {
    ID      string       `json:"id"`
    Object  string       `json:"object"`
    Created int64        `json:"created"`
    Model   string       `json:"model"`
    Choices []ChatChoice `json:"choices"`
    Usage   Usage        `json:"usage"`
}
```

**Response Format** (Streaming):
```go
type ChatCompletionStreamEvent struct {
    Chunk ChatCompletionChunk
    Err   error
}

type ChatCompletionChunk struct {
    ID      string       `json:"id"`
    Object  string       `json:"object"`
    Created int64        `json:"created"`
    Model   string       `json:"model"`
    Choices []ChatChoice `json:"choices"`
}
```

This unified format is OpenAI-compatible, which means:
- OpenAI providers require minimal translation
- Other providers translate their formats to match this structure
- The API layer can return responses directly without additional transformation

## Adapter Implementations

### OpenAI Adapter

The OpenAI adapter ([`internal/provider/openai/chat.go`](../../internal/provider/openai/chat.go)) implements the provider interfaces for OpenAI-compatible APIs:

**Configuration:**
```go
type Config struct {
    BaseURL      string        // API endpoint (e.g., "https://api.openai.com/v1")
    APIKey       string        // Provider API key
    DefaultModel string        // Fallback model if not specified
    HTTPClient   *http.Client  // Custom HTTP client (optional)
    Timeout      time.Duration // Request timeout
    MaxRetries   int           // Retry attempts for retryable errors
    RetryBackoff time.Duration // Delay between retries
}
```

**Key Features:**
- HTTP client with configurable timeout and retry logic
- Automatic retry for timeout, rate limit (429), and 5xx errors
- SSE streaming with proper chunk parsing and `[DONE]` marker handling
- Error extraction from OpenAI error response format

**Error Handling:**
The adapter normalizes OpenAI errors into Go errors:
```go
// Retryable errors are wrapped in a retryableError type
type retryableError struct {
    cause error
}

// HTTP status codes that trigger retry:
// - 408 Request Timeout
// - 429 Too Many Requests
// - 5xx Server Errors
```

### Mock Adapter

The mock adapter ([`internal/provider/mock/chat.go`](../../internal/provider/mock/chat.go)) provides a test implementation:

**Configuration:**
```go
type Config struct {
    ResponseText string   // Non-streaming response text
    StreamParts  []string // Streaming response chunks
    FailCreate   bool     // Simulate non-streaming failure
    FailStream   bool     // Simulate streaming failure
    Model        string   // Model ID to return
}
```

**Use Cases:**
- Local development without external API dependencies
- Integration testing with predictable responses
- Failure scenario testing (timeouts, errors, interruptions)
- Performance benchmarking without external rate limits

### Ollama Adapter

The Ollama adapter ([`internal/provider/ollama/chat.go`](../../internal/provider/ollama/chat.go)) connects the gateway to Ollama's local HTTP API while still returning the same internal response types used by the OpenAI-compatible adapter.

**Configuration:**
```go
type Config struct {
    BaseURL      string        // Ollama server root (for example "http://127.0.0.1:11434")
    APIKey       string        // Optional bearer token for proxied deployments
    DefaultModel string        // Fallback model if not specified
    HTTPClient   *http.Client  // Custom HTTP client (optional)
    Timeout      time.Duration // Request timeout
    MaxRetries   int           // Retry attempts for retryable errors
    RetryBackoff time.Duration // Delay between retries
}
```

**Key Features:**
- translates Ollama `POST /api/chat` responses into the unified completion shape
- converts newline-delimited streaming responses into `ChatCompletionStreamEvent` values
- exposes `GET /api/tags` through `ListModels()`
- reuses timeout and retry semantics for retryable HTTP and network failures

This matters because the adapter layer is no longer only validating OpenAI-compatible upstreams. It now proves that the gateway abstraction can normalize a materially different upstream protocol without changing the router or API handlers.

### Anthropic Adapter

The Anthropic adapter ([`internal/provider/anthropic/chat.go`](../../internal/provider/anthropic/chat.go)) connects the gateway to Anthropic's `/v1/messages` and `/v1/models` APIs while preserving the same internal `ChatCompletionProvider` and `ModelProvider` contracts used everywhere else in the codebase.

**Configuration:**
```go
type Config struct {
    BaseURL      string        // Anthropic API root (for example "https://api.anthropic.com/v1")
    APIKey       string        // Anthropic API key
    DefaultModel string        // Fallback model if not specified
    APIVersion   string        // anthropic-version header (defaults to "2023-06-01")
    MaxTokens    int           // Required by Anthropic messages API
    HTTPClient   *http.Client  // Custom HTTP client (optional)
    Timeout      time.Duration // Request timeout
    MaxRetries   int           // Retry attempts for retryable errors
    RetryBackoff time.Duration // Delay between retries
}
```

**Key Features:**
- sends Anthropic-specific `x-api-key` and `anthropic-version` headers automatically
- translates OpenAI-style `system` messages into Anthropic's top-level `system` field before forwarding the remaining `user` / `assistant` messages
- maps Anthropic text content blocks back into the unified assistant message shape
- parses Anthropic named SSE events such as `message_start`, `content_block_delta`, `message_delta`, and `message_stop`
- exposes `GET /v1/models` through `ListModels()`

This adapter matters because Anthropic is the first hosted upstream in the repo that is not merely OpenAI-compatible. It demonstrates that the gateway can normalize a materially different request shape, response schema, and streaming protocol without changing routing, auth, governance, or HTTP handlers.

## Error Semantic Normalization

Different providers return errors in different formats. The adapter layer normalizes these into consistent error semantics that the router can understand.

### Error Categories

**1. Retryable Errors**

Errors that should trigger retry logic:
- Network timeouts (`context.DeadlineExceeded`, `net.Error`)
- Rate limiting (HTTP 429)
- Server errors (HTTP 5xx)
- Temporary connection failures

The OpenAI adapter marks these errors as retryable:
```go
func shouldRetryHTTPStatus(status int) bool {
    return status == http.StatusRequestTimeout || 
           status == http.StatusTooManyRequests || 
           status >= http.StatusInternalServerError
}

func shouldRetryRequest(ctx context.Context, err error) bool {
    if err == nil || ctx.Err() != nil {
        return false
    }
    var retryable retryableError
    if errors.As(err, &retryable) {
        return true
    }
    if errors.Is(err, context.DeadlineExceeded) {
        return true
    }
    var netErr net.Error
    return errors.As(err, &netErr)
}
```

**2. Terminal Errors**

Errors that should not be retried:
- Authentication failures (HTTP 401)
- Invalid requests (HTTP 400)
- Resource not found (HTTP 404)
- Context cancellation

These errors are returned directly without retry.

**3. Streaming Errors**

Streaming requests have special error handling:
- Errors before the first chunk: Retryable, fallback allowed
- Errors after the first chunk: Terminal, no fallback (see [Streaming Proxy Design](streaming-proxy.md))

The router detects this by awaiting the first chunk:
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

### Error Message Extraction

The OpenAI adapter extracts error messages from provider responses:

```go
func readHTTPError(resp *http.Response) error {
    body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
    if err != nil {
        return fmt.Errorf("upstream status %d", resp.StatusCode)
    }

    var payload responsePayload
    if err := json.Unmarshal(body, &payload); err == nil && 
       payload.Error != nil && 
       payload.Error.Message != "" {
        return fmt.Errorf("upstream status %d: %s", resp.StatusCode, payload.Error.Message)
    }

    text := strings.TrimSpace(string(body))
    if text == "" {
        return fmt.Errorf("upstream status %d", resp.StatusCode)
    }

    return fmt.Errorf("upstream status %d: %s", resp.StatusCode, text)
}
```

This ensures error messages are informative regardless of provider response format.

## Router Integration

The provider router ([`internal/provider/router/chat.go`](../../internal/provider/router/chat.go)) uses the adapter abstraction to implement routing, fallback, and health tracking:

**Backend Configuration:**
```go
type Backend struct {
    Name     string
    Priority int
    Models   []string
    Provider provider.ChatCompletionProvider
}
```

The router treats all backends uniformly:
- Ranks backends using exact model matches and explicit priority
- Calls the same interface methods regardless of provider type
- Handles errors consistently based on normalized error semantics
- Tracks health state per backend without provider-specific logic

**Example: Non-Streaming Fallback**
```go
func (p *Provider) CreateChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (provider.ChatCompletionResponse, error) {
    candidates, skipped := p.candidates(req.Model)
    
    var lastErr error
    for index, backend := range candidates {
        resp, err := backend.Provider.CreateChatCompletion(ctx, req)
        if err == nil {
            p.markSuccess(backend.Name)
            return resp, nil
        }
        
        lastErr = err
        p.markFailure(backend.Name)
    }
    
    return provider.ChatCompletionResponse{}, lastErr
}
```

The router doesn't need to know whether it's calling OpenAI, Anthropic, Ollama, a mock, or any other provider—it just calls the interface method and handles the result.

## Adding New Providers

To add a new provider (e.g., Cohere, Bedrock, local models):

1. **Implement the interfaces** in a new package (e.g., `internal/provider/cohere/`)
2. **Translate request format** from the unified format to the provider's API format
3. **Translate response format** from the provider's API format to the unified format
4. **Normalize errors** into retryable vs. terminal categories
5. **Handle streaming** with proper SSE parsing and error propagation
6. **Configure in router** by adding a new backend with the provider instance

**Example structure:**
```go
package cohere

import "github.com/MoChengqian/llm-access-gateway/internal/provider"

type Config struct {
    APIKey       string
    BaseURL      string
    DefaultModel string
    // ... other config
}

type Provider struct {
    // ... internal state
}

func New(cfg Config) Provider {
    // ... initialization
}

func (p Provider) CreateChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (provider.ChatCompletionResponse, error) {
    // 1. Translate req to provider-specific format
    // 2. Call upstream API
    // 3. Translate response to unified format
    // 4. Normalize errors
}

func (p Provider) StreamChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (<-chan provider.ChatCompletionStreamEvent, error) {
    // 1. Translate req to provider-specific format
    // 2. Open upstream stream
    // 3. Parse SSE chunks
    // 4. Translate chunks to unified format
    // 5. Propagate errors properly
}

func (p Provider) ListModels(ctx context.Context) ([]provider.Model, error) {
    // Call provider models API
}
```

No changes to the router, API handlers, or governance layers are required—the adapter pattern isolates provider-specific logic.

## Design Trade-offs

**Advantages:**
- **Extensibility**: New providers can be added without modifying existing code
- **Testability**: Mock providers enable comprehensive testing without external dependencies
- **Maintainability**: Provider-specific logic is isolated in adapter packages
- **Consistency**: Unified error handling and response format across all providers

**Limitations:**
- **Feature Parity**: The unified interface supports only common features across providers (no provider-specific parameters)
- **Translation Overhead**: Some providers require format translation, adding minimal latency
- **OpenAI Bias**: The unified format is OpenAI-compatible, which may not perfectly match other providers' native formats

**Future Considerations:**
- Support for provider-specific parameters via passthrough fields
- Adapter-level caching for model lists
- Provider-specific retry strategies
- Support for non-chat endpoints (embeddings, fine-tuning, etc.)

## Related Documentation

- [Request Flow](request-flow.md) - How requests flow through the gateway layers
- [Streaming Proxy Design](streaming-proxy.md) - SSE streaming implementation details
- [Routing and Resilience](routing-resilience.md) - Routing, retry, and fallback strategies
- [Observability Design](observability.md) - Metrics, tracing, and logging for providers

## Code References

- Provider interfaces: [`internal/provider/provider.go`](../../internal/provider/provider.go)
- Anthropic adapter: [`internal/provider/anthropic/chat.go`](../../internal/provider/anthropic/chat.go)
- OpenAI adapter: [`internal/provider/openai/chat.go`](../../internal/provider/openai/chat.go)
- Ollama adapter: [`internal/provider/ollama/chat.go`](../../internal/provider/ollama/chat.go)
- Mock adapter: [`internal/provider/mock/chat.go`](../../internal/provider/mock/chat.go)
- Provider router: [`internal/provider/router/chat.go`](../../internal/provider/router/chat.go)
- Router tests: [`internal/provider/router/chat_test.go`](../../internal/provider/router/chat_test.go)
