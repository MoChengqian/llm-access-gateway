# API Reference

This document describes the current HTTP surface exposed by the repository.
All paths, headers, and examples are based on the current code.

Use this page after [Local Development](local-development.md). That page gets
the gateway running; this page tells you what to call first and what each
endpoint proves.

## Fast First Verification Path

If you only want the minimum first-pass API check, use these requests after
`go run ./cmd/devinit` and `go run ./cmd/gateway`:

```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer lag-local-dev-key'
curl -i 'http://127.0.0.1:8080/v1/usage?limit=5' \
  -H 'Authorization: Bearer lag-local-dev-key'
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'
curl -i -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}],"stream":true}'
```

That first-pass path proves:

- the process is live
- readiness is computed from provider health
- authenticated tenant-scoped endpoints work
- non-stream and stream chat completions both work
- the local seed key is valid for the current request path

If the first pass is green and you want the next layer of detail, continue with:

- [api/endpoints.md](api/endpoints.md) for the tenant-scoped HTTP contract and error boundaries
- [api/streaming.md](api/streaming.md) for SSE behavior, fallback limits, and TTFT checks
- [api/authentication.md](api/authentication.md) for API key requirements and tenant lookup rules

For the broader verification contract, read
[verification/README.md](verification/README.md).

## API Surface At A Glance

Operational endpoints:

- `GET /healthz`
- `GET /readyz`
- `GET /debug/providers`
- `GET /metrics`

Tenant-scoped endpoints:

- `GET /v1/models`
- `GET /v1/usage`
- `POST /v1/chat/completions`

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

The local first-run and first-verification language in this file intentionally
matches [Local Development](local-development.md) so the first request path and
the first API checks stay aligned.

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
