# PRD V1

## Week 1 Goal

Ship the minimum runnable gateway service that can start, expose basic health endpoints, and return a mock chat completion response through an OpenAI-compatible route.

## In scope

- Load config from `configs/config.yaml`
- Start a Go HTTP server
- Keep a clear `api -> service -> provider` layering
- Expose `GET /healthz`
- Expose `GET /readyz`
- Expose `POST /v1/chat/completions`
- Return a mock JSON completion response
- Use a mock chat provider behind a provider interface
- Emit structured request logs with request IDs

## Out of scope

- Real provider integration
- Streaming proxy behavior
- Authentication and tenant resolution
- Rate limiting, budgets, routing, fallback
- Persistence, migrations, or UI work

## Acceptance criteria

- `go run ./cmd/gateway` starts successfully
- `/healthz` returns `200`
- `/readyz` returns `200`
- `/v1/chat/completions` returns a mock completion JSON payload
- `make run`, `make test`, and `make fmt` are available
