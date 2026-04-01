# LLM Access Gateway

Minimal Week 1 runnable skeleton for a multi-tenant model access gateway in Go.

## What is included

- `chi` router with thin HTTP handlers
- `zap` logger for structured request logs
- `viper` config loading from `configs/config.yaml`
- `GET /healthz`
- `GET /readyz`
- `POST /v1/chat/completions` with a mock OpenAI-compatible JSON response

## Quick start

```bash
make run
```

Default address:

```text
:8080
```

Override the listen address with an environment variable:

```bash
APP_SERVER_ADDRESS=127.0.0.1:18080 make run
```

## Endpoints

Health:

```bash
curl http://127.0.0.1:8080/healthz
```

Readiness:

```bash
curl http://127.0.0.1:8080/readyz
```

Mock chat completion:

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'
```

## Development

```bash
make fmt
make test
```
