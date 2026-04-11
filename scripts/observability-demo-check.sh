#!/usr/bin/env bash

set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://127.0.0.1:9090}"
GRAFANA_URL="${GRAFANA_URL:-http://127.0.0.1:3000}"
GRAFANA_USERNAME="${GRAFANA_USERNAME:-admin}"
GRAFANA_PASSWORD="${GRAFANA_PASSWORD:-admin}"
OTEL_COLLECTOR_HEALTH_URL="${OTEL_COLLECTOR_HEALTH_URL:-http://127.0.0.1:13133}"
OTEL_COLLECTOR_METRICS_URL="${OTEL_COLLECTOR_METRICS_URL:-http://127.0.0.1:8888/metrics}"
WAIT_SECONDS="${WAIT_SECONDS:-30}"

print_section() {
  printf '\n== %s ==\n' "$1"
}

fail() {
  printf 'ERROR: %s\n' "$1" >&2
  exit 1
}

wait_for_http() {
  local url="$1"
  local label="$2"
  local i
  for i in $(seq 1 "${WAIT_SECONDS}"); do
    if curl -fsS "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  fail "${label} did not become ready at ${url}"
}

query_prometheus() {
  local query="$1"
  curl -fsS -G "${PROMETHEUS_URL}/api/v1/query" --data-urlencode "query=${query}"
}

grafana_api() {
  local path="$1"
  curl -fsS -u "${GRAFANA_USERNAME}:${GRAFANA_PASSWORD}" "${GRAFANA_URL}${path}"
}

wait_for_command_output() {
  local label="$1"
  local needle="$2"
  shift 2
  local output=""
  local i
  for i in $(seq 1 "${WAIT_SECONDS}"); do
    if output="$("$@" 2>/dev/null)" && [[ "${output}" == *"${needle}"* ]]; then
      printf '%s\n' "${output}"
      return 0
    fi
    sleep 1
  done
  printf '%s\n' "${output}"
  fail "${label} did not become ready"
}

print_section "wait for gateway"
wait_for_http "${BASE_URL}/healthz" "gateway"

print_section "wait for collector"
wait_for_http "${OTEL_COLLECTOR_HEALTH_URL}" "otel collector health"
wait_for_http "${OTEL_COLLECTOR_METRICS_URL}" "otel collector metrics"

print_section "wait for prometheus"
wait_for_http "${PROMETHEUS_URL}/-/ready" "prometheus"

print_section "wait for grafana"
wait_for_http "${GRAFANA_URL}/api/health" "grafana"

print_section "check grafana provisioning"
wait_for_command_output "grafana Prometheus datasource" '"type":"prometheus"' \
  grafana_api '/api/datasources/name/Prometheus'
wait_for_command_output "grafana llm-access-gateway dashboard" '"uid":"llm-access-gateway"' \
  grafana_api '/api/search?query=LLM%20Access%20Gateway'

print_section "trigger gateway traffic"
curl -fsS "${BASE_URL}/healthz" >/dev/null
curl -fsS "${BASE_URL}/metrics" >/dev/null
printf 'triggered gateway health and metrics requests via %s\n' "${BASE_URL}"

print_section "check prometheus query"
wait_for_command_output "prometheus lag_http_requests_total query" 'lag_http_requests_total' \
  query_prometheus 'lag_http_requests_total'

print_section "check collector accepted spans metric"
i=0
for i in $(seq 1 "${WAIT_SECONDS}"); do
  collector_metrics="$(curl -fsS "${OTEL_COLLECTOR_METRICS_URL}")"
  if [[ "${collector_metrics}" == *"otelcol_receiver_accepted_spans"* ]] &&
     ! printf '%s\n' "${collector_metrics}" | grep -Eq 'otelcol_receiver_accepted_spans(_total)?([^0-9]|$)[[:space:]]+0([[:space:]]|$)'; then
    printf '%s\n' "${collector_metrics}" | grep 'otelcol_receiver_accepted_spans' || true
    exit 0
  fi
  sleep 1
done

fail "otel collector did not report accepted spans after gateway traffic; ensure the gateway exports OTLP traces to http://127.0.0.1:4318/v1/traces"
