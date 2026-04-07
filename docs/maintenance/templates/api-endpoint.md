# API Endpoint Template

## Overview

Describe the endpoint in one paragraph:

- what the route does
- who should call it
- what stable contract the gateway guarantees today

## Route

```text
METHOD /path
```

State:

- whether authentication is required
- whether the endpoint is streaming or non-streaming
- whether the route is stable, draft, or internal-only

## Request

### Headers

List required and optional headers.

### Query Parameters

Document each supported query parameter, default behavior, and validation rule.

### Request Body

Show the exact request shape that the current handler accepts.

```json
{
  "field": "value"
}
```

Explain:

- required fields
- optional fields
- current defaults
- validation behavior

## Response

### Success Response

Provide a real example that matches the current implementation.

```json
{
  "object": "..."
}
```

### Streaming Response

If the route streams, document:

- `Content-Type`
- chunk shape
- end-of-stream marker
- fallback or interruption boundaries

```text
data: {...}

data: [DONE]
```

## Error Responses

Document actual status codes and error payloads emitted today.

| Status | When it happens | Example body |
|--------|------------------|--------------|
| 400 | invalid request | `{"error":"..."}` |
| 401 | auth failure | `{"error":"..."}` |

Use only statuses that are backed by the handler, middleware, or tests.

## Observability

Document any endpoint-specific observability behavior:

- `X-Request-Id`
- `X-Trace-Id`
- relevant `/metrics` counters
- structured log fields

## Verification

Provide commands that a reader can run immediately against `http://127.0.0.1:8080`.

```bash
curl -i http://127.0.0.1:8080/path \
  -H 'Authorization: Bearer lag-local-dev-key'
```

Include the expected success markers:

- HTTP status
- response content markers
- stream markers if applicable

## Related Documentation

- [Authentication](../../api/authentication.md)
- [API Endpoints](../../api/endpoints.md)

Replace this list with the most relevant neighboring documents.

## Code References

- [`internal/api/router.go`](../../../internal/api/router.go)
- [`internal/api/handlers/...`](../../../internal/api/handlers/)

Replace these entries with the exact files that implement the endpoint.
