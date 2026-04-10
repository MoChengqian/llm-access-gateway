# API Endpoints

This document provides detailed documentation for all HTTP endpoints exposed by the LLM Access Gateway.

## GET /v1/models

Lists all available models from configured providers. The gateway aggregates models from all active providers and returns a unified list.

### Authentication

**Required**: Yes

Include your API key in the Authorization header:

```
Authorization: Bearer <your-api-key>
```

### Request

**Method**: `GET`

**Path**: `/v1/models`

**Headers**:
- `Authorization: Bearer <api-key>` (required)

**Query Parameters**: None

### Response

**Status Code**: `200 OK`

**Headers**:
- `Content-Type: application/json`

**Response Body**:

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4o-mini",
      "object": "model",
      "created": 0,
      "owned_by": "mock-primary"
    },
    {
      "id": "gpt-4o",
      "object": "model",
      "created": 0,
      "owned_by": "mock-secondary"
    }
  ]
}
```

**Response Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `object` | string | Object type, always `"list"`. |
| `data` | array | Array of model objects. |
| `data[].id` | string | Unique identifier for the model (e.g., `"gpt-4o-mini"`). |
| `data[].object` | string | Object type, always `"model"`. |
| `data[].created` | integer | Unix timestamp of when the model was created. May be `0` for some providers. |
| `data[].owned_by` | string | The provider that owns this model (e.g., `"mock-primary"`, `"openai"`). |

### Behavior

- **Aggregation**: The gateway queries all configured providers and aggregates their model lists into a single response.
- **Partial Success**: If at least one provider succeeds, the gateway returns the aggregated results from successful providers.
- **Complete Failure**: Only when all providers fail does the endpoint return a `500` error.
- **Deduplication**: Models with the same ID from different providers are included separately, distinguished by the `owned_by` field.

### Error Responses

#### Authentication Errors

**Missing API Key**

**Status Code**: `401 Unauthorized`

```json
{
  "error": "missing api key"
}
```

**Invalid API Key**

**Status Code**: `401 Unauthorized`

```json
{
  "error": "invalid api key"
}
```

**Disabled API Key**

**Status Code**: `401 Unauthorized`

```json
{
  "error": "disabled api key"
}
```

See [Authentication](authentication.md) for detailed information about authentication errors.

#### Server Errors

**Internal Server Error**

**Status Code**: `500 Internal Server Error`

```json
{
  "error": "internal server error"
}
```

Returned when all configured providers fail to return model lists.

### Examples

#### Successful Request

```bash
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer lag-local-dev-key'
```

**Response**:

```
HTTP/1.1 200 OK
Content-Type: application/json

{
  "object": "list",
  "data": [
    {
      "id": "gpt-4o-mini",
      "object": "model",
      "created": 0,
      "owned_by": "mock-primary"
    },
    {
      "id": "gpt-4o",
      "object": "model",
      "created": 0,
      "owned_by": "mock-primary"
    }
  ]
}
```

#### Error Example: Missing API Key

```bash
curl -i http://127.0.0.1:8080/v1/models
```

**Response**:

```
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{
  "error": "missing api key"
}
```

#### Error Example: Invalid API Key

```bash
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer invalid-key-xyz'
```

**Response**:

```
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{
  "error": "invalid api key"
}
```

#### Error Example: Disabled API Key

```bash
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer disabled-key'
```

**Response**:

```
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{
  "error": "disabled api key"
}
```

### Notes

- **Provider Aggregation**: The gateway queries all configured providers in parallel and combines their model lists.
- **Resilience**: The endpoint returns partial results if some providers fail, ensuring availability even during partial outages.
- **No Governance**: Unlike chat completions, the models endpoint does not consume quota or count against rate limits.
- **Request ID**: All requests are assigned a unique request ID for tracing. Check the `X-Request-Id` response header.

### Related Documentation

- [Authentication](authentication.md) - Detailed authentication and API key management
- [POST /v1/chat/completions](endpoints.md#post-v1chatcompletions) - Create chat completions
- [Architecture: Provider Adapters](../architecture/provider-adapters.md) - Provider abstraction design

## GET /v1/usage

Returns tenant-level usage and quota information for the authenticated API key.

### Authentication

**Required**: Yes

Include your API key in the Authorization header:

```
Authorization: Bearer <your-api-key>
```

### Request

**Method**: `GET`

**Path**: `/v1/usage`

**Headers**:
- `Authorization: Bearer <api-key>` (required)

**Query Parameters**:

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `limit` | integer | No | Number of recent usage records to return. Defaults to `20` and is capped at `100`. |

### Response

**Status Code**: `200 OK`

**Headers**:
- `Content-Type: application/json`

**Response Body**:

```json
{
  "object": "usage",
  "tenant": {
    "id": 1,
    "name": "local-dev"
  },
  "summary": {
    "window_seconds": 60,
    "requests_last_minute": 0,
    "tokens_last_minute": 0,
    "total_tokens_used": 47,
    "logical_tokens_last_minute": 42,
    "logical_total_tokens_used": 42,
    "rpm_limit": 60,
    "tpm_limit": 4000,
    "token_budget": 1000000,
    "remaining_token_budget": 999953
  },
  "data": [
    {
      "request_id": "1775464165887288000-8",
      "api_key_id": 1,
      "model": "gpt-4o-mini",
      "stream": true,
      "status": "succeeded",
      "prompt_tokens": 3,
      "completion_tokens": 9,
      "total_tokens": 12,
      "created_at": "2026-04-06T08:29:26Z",
      "updated_at": "2026-04-06T08:29:25Z"
    }
  ]
}
```

### Behavior

- **Window Summary**: `summary.requests_last_minute` covers logical client requests in the last 60 seconds, while `summary.tokens_last_minute` covers provider-attempt token usage in the same window.
- **Dual Token Views**: `summary.logical_tokens_last_minute` and `summary.logical_total_tokens_used` reflect logical request records from `request_usages`; `summary.tokens_last_minute` and `summary.total_tokens_used` include retry and fallback attempt usage.
- **Budget View**: `remaining_token_budget` is derived from the tenant budget minus provider-attempt token usage.
- **Recent History**: `data` returns recent `request_usages` records for the authenticated tenant only.
- **Limit Handling**: `limit=0` is treated as the default, negative or non-integer values are rejected, and values above `100` are capped.

### Error Responses

#### Authentication Errors

**Missing API Key**

**Status Code**: `401 Unauthorized`

```json
{
  "error": "missing api key"
}
```

**Invalid API Key**

**Status Code**: `401 Unauthorized`

```json
{
  "error": "invalid api key"
}
```

#### Request Errors

**Invalid Limit**

**Status Code**: `400 Bad Request`

```json
{
  "error": "invalid limit"
}
```

#### Server Errors

**Internal Server Error**

**Status Code**: `500 Internal Server Error`

```json
{
  "error": "internal server error"
}
```

### Examples

#### Successful Request

```bash
curl -i http://127.0.0.1:8080/v1/usage?limit=5 \
  -H 'Authorization: Bearer lag-local-dev-key'
```

#### Error Example: Invalid Limit

```bash
curl -i http://127.0.0.1:8080/v1/usage?limit=bad \
  -H 'Authorization: Bearer lag-local-dev-key'
```

**Response**:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json

{
  "error": "invalid limit"
}
```

### Notes

- **Tenant Scope**: The endpoint always returns data for the authenticated tenant; there is no cross-tenant access path.
- **Quota Insight**: This is the easiest endpoint to check when debugging RPM, TPM, and budget enforcement.
- **Non-Mutating**: The endpoint does not consume quota by itself.
- **Request ID**: The response carries `X-Request-Id` and `X-Trace-Id` like the other API endpoints.

### Related Documentation

- [Authentication](authentication.md) - API key requirements and tenant lookup
- [Architecture: Governance](../architecture/governance.md) - RPM, TPM, and budget model
- [Quota Enforcement Drill](../verification/failure-drills/quota-enforcement.md) - Observed rejection behavior

## POST /v1/chat/completions

Creates a chat completion for the provided messages. Supports both non-streaming and streaming responses.

### Authentication

**Required**: Yes

Include your API key in the Authorization header:

```
Authorization: Bearer <your-api-key>
```

### Request

**Method**: `POST`

**Path**: `/v1/chat/completions`

**Headers**:
- `Authorization: Bearer <api-key>` (required)
- `Content-Type: application/json` (required)

**Request Body**:

```json
{
  "model": "string",
  "messages": [
    {
      "role": "string",
      "content": "string"
    }
  ],
  "stream": boolean
}
```

**Parameters**:

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `model` | string | No | The model to use for completion. If not specified, the gateway's default model is used. |
| `messages` | array | Yes | Array of message objects representing the conversation history. Must contain at least one message. |
| `messages[].role` | string | Yes | The role of the message author. Typically `"user"`, `"assistant"`, or `"system"`. |
| `messages[].content` | string | Yes | The content of the message. |
| `stream` | boolean | No | Whether to stream the response using Server-Sent Events (SSE). Defaults to `false`. |

### Response

#### Non-Streaming Response (stream=false)

**Status Code**: `200 OK`

**Headers**:
- `Content-Type: application/json`

**Response Body**:

```json
{
  "id": "chatcmpl-123",
  "object": "chat.completion",
  "created": 1677652288,
  "model": "gpt-4o-mini",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hello! How can I help you today?"
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 9,
    "completion_tokens": 12,
    "total_tokens": 21
  }
}
```

**Response Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique identifier for the completion. |
| `object` | string | Object type, always `"chat.completion"`. |
| `created` | integer | Unix timestamp of when the completion was created. |
| `model` | string | The model used for the completion. |
| `choices` | array | Array of completion choices. Typically contains one choice. |
| `choices[].index` | integer | The index of the choice in the array. |
| `choices[].message` | object | The generated message. |
| `choices[].message.role` | string | The role of the message author, typically `"assistant"`. |
| `choices[].message.content` | string | The content of the generated message. |
| `choices[].finish_reason` | string | The reason the completion finished (e.g., `"stop"`, `"length"`). |
| `usage` | object | Token usage information. |
| `usage.prompt_tokens` | integer | Number of tokens in the prompt. |
| `usage.completion_tokens` | integer | Number of tokens in the completion. |
| `usage.total_tokens` | integer | Total number of tokens used (prompt + completion). |

#### Streaming Response (stream=true)

**Status Code**: `200 OK`

**Headers**:
- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`
- `Connection: keep-alive`

**Response Format**:

The response is sent as a series of Server-Sent Events (SSE). Each event is prefixed with `data: ` and contains a JSON object representing a chunk of the completion.

**Chunk Format**:

```json
{
  "id": "chatcmpl-123",
  "object": "chat.completion.chunk",
  "created": 1677652288,
  "model": "gpt-4o-mini",
  "choices": [
    {
      "index": 0,
      "delta": {
        "role": "assistant",
        "content": "Hello"
      },
      "finish_reason": null
    }
  ]
}
```

**Chunk Fields**:

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique identifier for the completion. |
| `object` | string | Object type, always `"chat.completion.chunk"`. |
| `created` | integer | Unix timestamp of when the completion was created. |
| `model` | string | The model used for the completion. |
| `choices` | array | Array of completion choices. |
| `choices[].index` | integer | The index of the choice in the array. |
| `choices[].delta` | object | The incremental content for this chunk. |
| `choices[].delta.role` | string | The role (present in first chunk only). |
| `choices[].delta.content` | string | The incremental content for this chunk. |
| `choices[].finish_reason` | string | The reason the completion finished. `null` until the final chunk. |

**Stream Termination**:

The stream is terminated with a special `[DONE]` marker:

```
data: [DONE]
```

**Example Stream**:

```
data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

### Error Responses

#### Authentication Errors

**Missing API Key**

**Status Code**: `401 Unauthorized`

```json
{
  "error": "missing api key"
}
```

**Invalid API Key**

**Status Code**: `401 Unauthorized`

```json
{
  "error": "invalid api key"
}
```

**Disabled API Key**

**Status Code**: `401 Unauthorized`

```json
{
  "error": "disabled api key"
}
```

#### Governance Errors

**Rate Limit Exceeded (RPM)**

**Status Code**: `429 Too Many Requests`

```json
{
  "error": "rate limit exceeded"
}
```

Returned when the tenant has exceeded their requests-per-minute (RPM) quota.

**Token Rate Limit Exceeded (TPM)**

**Status Code**: `429 Too Many Requests`

```json
{
  "error": "token rate limit exceeded"
}
```

Returned when the tenant has exceeded their tokens-per-minute (TPM) quota.

**Budget Exceeded**

**Status Code**: `403 Forbidden`

```json
{
  "error": "budget exceeded"
}
```

Returned when the tenant has exhausted their token budget.

#### Request Errors

**Invalid Request Body**

**Status Code**: `400 Bad Request`

```json
{
  "error": "invalid request body"
}
```

Returned when the request body is malformed or cannot be parsed as JSON.

**Request Body Too Large**

**Status Code**: `413 Request Entity Too Large`

```json
{
  "error": "request body too large"
}
```

Returned when the request body exceeds the maximum allowed size.

**Missing Messages**

**Status Code**: `400 Bad Request`

```json
{
  "error": "messages are required"
}
```

Returned when the `messages` array is empty or missing.

#### Server Errors

**Internal Server Error**

**Status Code**: `500 Internal Server Error`

```json
{
  "error": "internal server error"
}
```

Returned when an unexpected error occurs during request processing.

**Streaming Unsupported**

**Status Code**: `500 Internal Server Error`

```json
{
  "error": "streaming unsupported"
}
```

Returned when streaming is requested but the server does not support it (rare).

### Examples

#### Non-Streaming Request

```bash
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      {
        "role": "user",
        "content": "What is the capital of France?"
      }
    ],
    "stream": false
  }'
```

**Response**:

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion",
  "created": 1677652288,
  "model": "gpt-4o-mini",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "The capital of France is Paris."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 14,
    "completion_tokens": 8,
    "total_tokens": 22
  }
}
```

#### Streaming Request

```bash
curl -i -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      {
        "role": "user",
        "content": "Count to three"
      }
    ],
    "stream": true
  }'
```

**Response**:

```
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"One"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":","},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":" two"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":","},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":" three"},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{"content":"."},"finish_reason":null}]}

data: {"id":"chatcmpl-abc123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4o-mini","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

#### Multi-Turn Conversation

```bash
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{
    "messages": [
      {
        "role": "system",
        "content": "You are a helpful assistant."
      },
      {
        "role": "user",
        "content": "What is 2+2?"
      },
      {
        "role": "assistant",
        "content": "2+2 equals 4."
      },
      {
        "role": "user",
        "content": "What about 3+3?"
      }
    ]
  }'
```

#### Error Example: Missing API Key

```bash
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "messages": [
      {
        "role": "user",
        "content": "hello"
      }
    ]
  }'
```

**Response**:

```
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{
  "error": "missing api key"
}
```

#### Error Example: Invalid Request

```bash
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{
    "messages": []
  }'
```

**Response**:

```
HTTP/1.1 400 Bad Request
Content-Type: application/json

{
  "error": "messages are required"
}
```

### Notes

- **Model Selection**: If the `model` parameter is omitted, the gateway uses its configured default model.
- **Streaming Behavior**: When `stream=true`, the response is sent incrementally as Server-Sent Events. The client must support SSE to consume streaming responses.
- **TTFT Measurement**: For streaming requests, the gateway measures Time To First Token (TTFT) - the time from request start to the first chunk being sent.
- **Fallback Constraint**: Provider fallback is only possible before the first chunk is sent in streaming mode. Once streaming begins, the gateway is committed to the selected provider.
- **Request ID**: All requests are assigned a unique request ID for tracing and debugging. Check the `X-Request-Id` response header.
- **Token Counting**: Token usage is tracked for governance purposes. Prompt tokens are estimated before the request, and completion tokens are counted from the provider response.
- **Governance Checks**: All requests are subject to RPM, TPM, and budget checks before being forwarded to the provider.

### Related Documentation

- [Authentication](authentication.md) - Detailed authentication and API key management
- [SSE Streaming Format](streaming.md) - Deep dive into streaming implementation
- [Architecture: Governance](../architecture/governance.md) - Multi-tenant governance model
- [Architecture: Streaming Proxy](../architecture/streaming-proxy.md) - Streaming proxy design
