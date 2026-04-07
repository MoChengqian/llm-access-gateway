# Docker Compose Deployment

## Overview

The repository includes a Docker Compose stack for local and single-host deployment in [`deployments/docker/docker-compose.yml`](../../deployments/docker/docker-compose.yml). The stack packages four services:

- `mysql`: MySQL 8.4 for tenants, API keys, and request usage records
- `redis`: Redis 7.4 for RPM/TPM limiter counters
- `devinit`: one-shot bootstrap job that seeds the schema and a development tenant/API key
- `gateway`: the HTTP API service on port `8080`

This is the fastest path to bring up a realistic environment with MySQL-backed auth, Redis-backed governance, health endpoints, and the mock provider defaults used throughout the repo docs.

## Stack Topology

The Compose file wires the services in a strict dependency order:

```text
mysql  ─┐
        ├─> devinit ─┐
redis ──┘            ├─> gateway
mysql ───────────────┘
redis ───────────────┘
```

Important implementation details:

- `mysql` uses a named volume at `/var/lib/mysql`
- `redis` runs with persistence disabled (`--save "" --appendonly no`)
- `devinit` runs `"/app/devinit"` and exits once initialization succeeds
- `gateway` waits for healthy `mysql`, healthy `redis`, and successful `devinit`
- `gateway` publishes `8080`, `mysql` publishes `3306`, and `redis` publishes `6379`

## Bring Up the Stack

To build and start the full stack:

```bash
docker compose -f deployments/docker/docker-compose.yml up -d --build
docker compose -f deployments/docker/docker-compose.yml ps
docker compose -f deployments/docker/docker-compose.yml logs devinit
```

Expected steady-state result:

- `llm-access-gateway-mysql` is `healthy`
- `llm-access-gateway-redis` is `healthy`
- `llm-access-gateway-devinit` exits with code `0`
- `llm-access-gateway` becomes `healthy`

If you only want to validate the Compose model without starting containers, this command expands and validates the Compose file shape:

```bash
docker compose -f deployments/docker/docker-compose.yml config
```

That command was used during documentation verification to confirm the file expands correctly with the current repo layout.

## Environment and Service Wiring

The Compose file injects these application settings into the containers:

### `devinit`

```text
APP_MYSQL_DSN=user:pass@tcp(mysql:3306)/llm_access_gateway?parseTime=true
```

### `gateway`

```text
APP_SERVER_ADDRESS=:8080
APP_MYSQL_DSN=user:pass@tcp(mysql:3306)/llm_access_gateway?parseTime=true
APP_REDIS_ADDRESS=redis:6379
```

The gateway then loads the rest of its defaults from [`configs/config.yaml`](../../configs/config.yaml) and [`internal/config/config.go`](../../internal/config/config.go).

## Health and Readiness Checks

Compose defines health checks on three services:

### MySQL

```text
mysqladmin ping -h 127.0.0.1 -uuser -ppass --silent
```

### Redis

```text
redis-cli ping
```

### Gateway

```text
wget -qO- http://127.0.0.1:8080/healthz
```

The gateway health check uses `/healthz`, which only reports process liveness. Readiness is a separate application concern and should be checked explicitly with:

```bash
curl -i http://127.0.0.1:8080/readyz
curl -i http://127.0.0.1:8080/debug/providers
```

That distinction matters because the process can be alive while all provider backends are cooling down and `/readyz` returns `503`.

## Post-Startup Verification

After the stack is up, run the basic contract checks:

```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/metrics
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer lag-local-dev-key'
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer lag-local-dev-key' \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"hello"}]}'
```

For a broader acceptance run, the repository already includes:

```bash
./scripts/gateway-smoke-check.sh
ASSERT=true ./scripts/gateway-smoke-check.sh
make verify
```

These commands exercise:

- `/healthz`
- `/metrics`
- `/v1/models`
- `/v1/usage`
- non-stream chat completions
- stream chat completions
- the built-in load test

## Troubleshooting

### `mysql` or `redis` never becomes healthy

Inspect the container logs:

```bash
docker compose -f deployments/docker/docker-compose.yml logs mysql
docker compose -f deployments/docker/docker-compose.yml logs redis
```

Common causes:

- local ports `3306` or `6379` are already in use
- Docker Desktop is not fully started
- a previous failed container is holding stale state

### `devinit` exits non-zero

Inspect:

```bash
docker compose -f deployments/docker/docker-compose.yml logs devinit
```

Typical causes:

- MySQL is not actually ready yet
- the image build failed
- the DSN in the container environment does not match the database container

### `gateway` stays unhealthy

Inspect:

```bash
docker compose -f deployments/docker/docker-compose.yml logs gateway
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
curl -i http://127.0.0.1:8080/debug/providers
```

If `/healthz` is `200` but `/readyz` is `503`, the service is running but provider readiness is degraded.

## Shutdown and Cleanup

Stop the stack:

```bash
docker compose -f deployments/docker/docker-compose.yml down
```

Remove the MySQL volume as well:

```bash
docker compose -f deployments/docker/docker-compose.yml down -v
```

## Related Documentation

- [Local Development](../local-development.md)
- [Configuration Reference](configuration.md)
- [Production Considerations](production-considerations.md)
- [API Endpoints](../api/endpoints.md)

## Code References

- [`deployments/docker/docker-compose.yml`](../../deployments/docker/docker-compose.yml)
- [`cmd/devinit/main.go`](../../cmd/devinit/main.go)
- [`cmd/gateway/main.go`](../../cmd/gateway/main.go)
- [`scripts/gateway-smoke-check.sh`](../../scripts/gateway-smoke-check.sh)
- [`internal/api/handlers/health.go`](../../internal/api/handlers/health.go)
