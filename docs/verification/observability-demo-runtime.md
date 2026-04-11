# Observability Demo Runtime Verification

## Goal

Prove that the repository-owned local observability stack can verify metrics,
trace export, and Grafana provisioning in one command.

## Command

```bash
./scripts/observability-demo-verify.sh
```

The script performs the full local loop:

- warms the OpenTelemetry Collector, Prometheus, and Grafana images
- starts MySQL and Redis
- runs `go run ./cmd/devinit`
- starts the observability stack
- starts the gateway with OTLP/HTTP trace export enabled
- runs `scripts/observability-demo-check.sh`
- stops all containers and the host gateway process on exit

## Verified Locally

Date: 2026-04-11

Environment notes:

- OpenTelemetry Collector image: `otel/opentelemetry-collector-contrib:0.143.0`
- Prometheus image: `prom/prometheus:v3.9.1`
- Grafana image: `grafana/grafana:12.3.1`
- Prometheus host URL: `http://127.0.0.1:19090`
- Grafana host URL: `http://127.0.0.1:13000`
- OTLP traces endpoint: `http://127.0.0.1:4318/v1/traces`

Key evidence from the successful run:

```text
== check grafana provisioning ==
..."name":"Prometheus","type":"prometheus","url":"http://prometheus:9090"...
..."uid":"llm-access-gateway","title":"LLM Access Gateway","type":"dash-db"...

== check prometheus query ==
..."__name__":"lag_http_requests_total","instance":"host.docker.internal:8080"...

== check collector accepted spans metric ==
otelcol_receiver_accepted_spans{receiver="otlp",transport="http"} 4

== observability demo verify passed ==
base_url=http://127.0.0.1:8080
prometheus_url=http://127.0.0.1:19090
grafana_url=http://127.0.0.1:13000
otlp_traces_endpoint=http://127.0.0.1:4318/v1/traces
```

## Fixes Captured By This Verification

Two local-runtime assumptions were corrected during this verification:

- The OpenTelemetry Collector internal metrics config uses the current
  `service.telemetry.metrics.readers.pull.exporter.prometheus` shape instead of
  the old `service.telemetry.metrics.address` key.
- The Prometheus and Grafana host ports default to `19090` and `13000` to avoid
  common local conflicts such as proxy tools on `9090` and web dev servers on
  `3000`.
