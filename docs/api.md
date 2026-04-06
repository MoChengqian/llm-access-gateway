# API Reference

This document describes the current HTTP surface exposed by the repository.
All paths, headers, and examples are based on the current code.

Base URL for local development:

```text
http://127.0.0.1:8080
```

Authentication header for protected endpoints:

```text
Authorization: Bearer <api-key>
```

Current local development key after `go run ./cmd/devinit`:

```text
lag-local-dev-key
```

## `GET /healthz`

Purpose:

- liveness check

Auth:

- not required

Success response:

```json
{"status":"ok"}
```

## `GET /readyz`

Purpose:

- readiness check
- returns `503` when all providers are in cooldown or otherwise unavailable

Auth:

- not required

Success response:

```json
{"status":"ready"}
```

Failure response:

```json
{"status":"not ready"}
```

## `GET /debug/providers`

Purpose:

- inspect current backend health, cooldown, and probe state

Auth:

- not required

Success response shape:

```json
{
  "ready": true,
  "providers": [
    {
      "name": "mock-primary",
      "healthy": true,
      "consecutive_failures": 0,
      "unhealthy_until": "0001-01-01T00:00:00Z",
      "last_probe_at": "2026-04-06T10:00:00Z",
      "last_probe_error": ""
    }
  ]
}
```

## `GET /metrics`

Purpose:

- expose Prometheus-style metrics

Auth:

- not required

Examples of current metrics:

- `lag_http_requests_total`
- `lag_provider_events_total`
- `lag_readyz_failures_total`
- `lag_governance_rejections_total`
- `lag_stream_requests_total`
- `lag_stream_chunks_total`
- `lag_stream_ttft_milliseconds_sum`
- `lag_provider_operation_duration_milliseconds_count`
- `lag_provider_probe_results_total`

## `GET /v1/models`

Purpose:

- list aggregated models from configured providers

Auth:

- required

Notes:

- if at least one model source succeeds, the gateway returns partial aggregated results
- only when all sources fail does the endpoint return `500`

Success response shape:

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4o-mini",
      "object": "model",
      "created": 0,
      "owned_by": "mock-primary"
    }
  ]
}
```

## `GET /v1/usage`

Purpose:

- return the authenticated tenant's current quota summary and recent request usage records

Auth:

- required

Query parameters:

- `limit`
  - optional
  - default: `20`
  - max: `100`

Example:

```bash
curl -i 'http://127.0.0.1:8080/v1/usage?limit=5' \
  -H 'Authorization: Bearer lag-local-dev-key'
```

Success response shape:

```json
{
  "object": "usage",
  "tenant": {
    "id": 1,
    "name": "local-dev"
  },
  "summary": {
    "window_seconds": 60,
    "requests_last_minute": 2,
    "tokens_last_minute": 42,
    "total_tokens_used": 140,
    "rpm_limit": 60,
    "tpm_limit": 4000,
    "token_budget": 1000000,
    "remaining_token_budget": 999860
  },
  "data": [
    {
      "request_id": "req-123",
      "api_key_id": 1,
      "model": "gpt-4o-mini",
      "stream": false,
      "status": "succeeded",
      "prompt_tokens": 3,
      "completion_tokens": 8,
      "total_tokens": 11,
      "created_at": "2026-04-06T10:00:00Z",
      "updated_at": "2026-04-06T10:00:01Z"
    }
  ]
}
```

## `POST /v1/chat/completions`

Purpose:

- create a chat completion
- supports both non-stream and stream mode

Auth:

- required

Non-stream request:

```bash
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'
```

Stream request:

```bash
curl -i -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}],"stream":true}'
```

Current auth and governance errors:

- missing key -> `401 {"error":"missing api key"}`
- invalid key -> `401 {"error":"invalid api key"}`
- disabled key -> `401 {"error":"disabled api key"}`
- RPM exceeded -> `429 {"error":"rate limit exceeded"}`
- TPM exceeded -> `429 {"error":"token rate limit exceeded"}`
- budget exceeded -> `403 {"error":"budget exceeded"}`

Current stream behavior:

- `Content-Type: text/event-stream`
- multiple `data:` events
- final `data: [DONE]` on normal completion
- if upstream breaks after the first chunk, the gateway closes the stream without a false `[DONE]`
