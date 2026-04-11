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
make observability-demo-config
make observability-demo-up
make observability-demo-check
```

Services:

- Grafana: `http://127.0.0.1:3000`
- Prometheus: `http://127.0.0.1:9090`
- OpenTelemetry Collector health: `http://127.0.0.1:13133`
- OpenTelemetry Collector metrics: `http://127.0.0.1:8888/metrics`

Default Grafana login:

- username: `admin`
- password: `admin`

## What The Smoke Check Verifies

`scripts/observability-demo-check.sh` waits for:

- gateway `/healthz`
- collector health and metrics endpoints
- Prometheus `/-/ready`
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
