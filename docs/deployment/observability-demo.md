# Local Observability Demo Stack

## Goal

Run a repository-owned observability stack that proves three things together:

- the gateway can expose Prometheus metrics on `/metrics`
- the gateway can export traces over OTLP/HTTP to a real collector
- Grafana can boot with the repository dashboard and datasource wiring already provisioned

This stack is intentionally local and lightweight. It does **not** claim to be a
production Prometheus, Grafana, or trace-storage deployment.

## Stack Contents

- OpenTelemetry Collector
- Prometheus
- Grafana

Compose file:

```text
deployments/observability/docker-compose.yml
```

## Start The Gateway

Run the gateway on the host with OTLP export enabled:

```bash
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'
export APP_OBSERVABILITY_OTLP_TRACES_ENDPOINT='http://127.0.0.1:4318/v1/traces'
export APP_OBSERVABILITY_OTLP_EXPORT_TIMEOUT_SECONDS='1'
go run ./cmd/devinit
go run ./cmd/gateway
```

The demo stack assumes the gateway metrics stay on `http://127.0.0.1:8080/metrics`,
which Prometheus reaches as `host.docker.internal:8080`.

## Start The Demo Stack

```bash
make observability-demo-prepull
make observability-demo-config
make observability-demo-up
make observability-demo-check
make observability-demo-verify
```

Services:

- Grafana: `http://127.0.0.1:13000`
- Prometheus: `http://127.0.0.1:19090`
- OpenTelemetry Collector health: `http://127.0.0.1:13133`
- OpenTelemetry Collector metrics: `http://127.0.0.1:8888/metrics`

The UI ports intentionally avoid common local development defaults:

- `PROMETHEUS_HOST_PORT` defaults to `19090`
- `GRAFANA_HOST_PORT` defaults to `13000`

Default Grafana login:

- username: `admin`
- password: `admin`

`make observability-demo-prepull` retries the three observability images and is
the right first step on machines where Docker Hub or a mirror is slow.

`make observability-demo-verify` is the repository-owned end-to-end runtime
entry. It starts MySQL and Redis, seeds the database, launches the gateway with
OTLP export enabled, runs the observability demo check, and cleans up
afterward.

Latest runtime evidence is tracked in
[`docs/verification/observability-demo-runtime.md`](../verification/observability-demo-runtime.md).

## What The Smoke Check Verifies

`scripts/observability-demo-check.sh` waits for:

- gateway `/healthz`
- collector health and metrics endpoints
- Prometheus `/api/v1/status/runtimeinfo`
- Grafana `/api/health`

Then it:

- triggers gateway traffic
- queries Prometheus for `lag_http_requests_total`
- checks the collector internal metrics for a non-zero
  `otelcol_receiver_accepted_spans` signal
- checks Grafana for the provisioned `Prometheus` datasource and
  `llm-access-gateway` dashboard

That means the repository can prove metrics scraping and OTLP trace ingestion in
one local loop, while also proving Grafana booted with the repo-owned
provisioning assets.

## Shutdown

```bash
make observability-demo-down
```

## Scope Boundary

This stack intentionally stops short of a persisted trace backend such as Tempo.
The goal is a repo-native demo environment, not a production observability
platform. If you later need trace search, retention, multi-user auth, ingress,
or secret management, treat that as a separate hardening layer.
