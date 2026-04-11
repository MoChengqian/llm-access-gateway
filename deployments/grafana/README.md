# Grafana Dashboard Assets

This directory contains importable Grafana assets for the gateway metrics
contract exposed by `GET /metrics`.

## Dashboard

- `dashboards/llm-access-gateway.json`

Import the dashboard into Grafana and bind it to a Prometheus data source named
`Prometheus`, or change the dashboard variable to match your data source name.

The panels use the Stage 6 metric contract:

- `lag_http_requests_total`
- `lag_http_request_duration_milliseconds_sum`
- `lag_http_request_duration_milliseconds_count`
- `lag_provider_events_total`
- `lag_provider_operation_duration_milliseconds_sum`
- `lag_provider_operation_duration_milliseconds_count`
- `lag_provider_backend_healthy`
- `lag_provider_ready`
- `lag_governance_rejections_total`
- `lag_stream_ttft_milliseconds_sum`
- `lag_stream_ttft_milliseconds_count`

The dashboard intentionally avoids percentile panels because the current in-process
registry publishes count/sum pairs, not histogram buckets.
