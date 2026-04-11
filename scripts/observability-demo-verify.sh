#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MAIN_COMPOSE_FILE="${REPO_ROOT}/deployments/docker/docker-compose.yml"
OBS_COMPOSE_FILE="${REPO_ROOT}/deployments/observability/docker-compose.yml"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://127.0.0.1:19090}"
GRAFANA_URL="${GRAFANA_URL:-http://127.0.0.1:13000}"
MYSQL_DSN="${APP_MYSQL_DSN:-user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true}"
REDIS_ADDRESS="${APP_REDIS_ADDRESS:-127.0.0.1:6379}"
OTLP_TRACES_ENDPOINT="${APP_OBSERVABILITY_OTLP_TRACES_ENDPOINT:-http://127.0.0.1:4318/v1/traces}"
OTLP_EXPORT_TIMEOUT_SECONDS="${APP_OBSERVABILITY_OTLP_EXPORT_TIMEOUT_SECONDS:-1}"
SERVICE_NAME="${APP_OBSERVABILITY_SERVICE_NAME:-llm-access-gateway}"
WAIT_SECONDS="${WAIT_SECONDS:-45}"
PREPULL_IMAGES="${PREPULL_IMAGES:-true}"
GOCACHE_DIR="${GOCACHE_DIR:-/tmp/lag-observability-demo-gocache}"
GOMODCACHE_DIR="${GOMODCACHE_DIR:-/tmp/lag-observability-demo-gomodcache}"
GATEWAY_LOG="${GATEWAY_LOG:-/tmp/lag-observability-demo-gateway.log}"

gateway_pid=""

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

wait_for_container_health() {
  local container_name="$1"
  local label="$2"
  local status=""
  local i
  for i in $(seq 1 "${WAIT_SECONDS}"); do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "${container_name}" 2>/dev/null || true)"
    if [[ "${status}" == "healthy" || "${status}" == "running" ]]; then
      printf '%s status=%s\n' "${label}" "${status}"
      return 0
    fi
    sleep 1
  done
  fail "${label} did not become healthy; last status=${status:-unknown}"
}

cleanup() {
  local status=$?

  if [[ -n "${gateway_pid}" ]]; then
    kill "${gateway_pid}" >/dev/null 2>&1 || true
    wait "${gateway_pid}" 2>/dev/null || true
  fi

  docker compose -f "${OBS_COMPOSE_FILE}" down >/dev/null 2>&1 || true
  docker compose -f "${MAIN_COMPOSE_FILE}" down >/dev/null 2>&1 || true

  if [[ "${status}" -ne 0 ]]; then
    printf '\n-- gateway log tail --\n' >&2
    tail -n 80 "${GATEWAY_LOG}" >&2 || true
    printf '\n-- observability compose ps --\n' >&2
    docker compose -f "${OBS_COMPOSE_FILE}" ps >&2 || true
    printf '\n-- runtime compose ps --\n' >&2
    docker compose -f "${MAIN_COMPOSE_FILE}" ps >&2 || true
  fi

  exit "${status}"
}

trap cleanup EXIT

cd "${REPO_ROOT}"

print_section "preflight"
if curl -fsS "${BASE_URL}/healthz" >/dev/null 2>&1; then
  fail "${BASE_URL} is already serving traffic; run the demo verify in a clean environment"
fi
rm -f "${GATEWAY_LOG}"

if [[ "${PREPULL_IMAGES}" == "true" ]]; then
  print_section "prepull observability images"
  ./scripts/observability-demo-prepull.sh
fi

print_section "start mysql and redis"
docker compose -f "${MAIN_COMPOSE_FILE}" up -d mysql redis
wait_for_container_health "llm-access-gateway-mysql" "mysql"
wait_for_container_health "llm-access-gateway-redis" "redis"

print_section "bootstrap schema and seed data"
APP_MYSQL_DSN="${MYSQL_DSN}" GOCACHE="${GOCACHE_DIR}" GOMODCACHE="${GOMODCACHE_DIR}" go run ./cmd/devinit

print_section "start observability demo stack"
docker compose -f "${OBS_COMPOSE_FILE}" up -d
wait_for_http "http://127.0.0.1:13133" "otel collector health"
wait_for_http "${PROMETHEUS_URL}/api/v1/status/runtimeinfo" "prometheus"
wait_for_http "${GRAFANA_URL}/api/health" "grafana"

print_section "start gateway"
nohup env \
  APP_MYSQL_DSN="${MYSQL_DSN}" \
  APP_REDIS_ADDRESS="${REDIS_ADDRESS}" \
  APP_OBSERVABILITY_SERVICE_NAME="${SERVICE_NAME}" \
  APP_OBSERVABILITY_OTLP_TRACES_ENDPOINT="${OTLP_TRACES_ENDPOINT}" \
  APP_OBSERVABILITY_OTLP_EXPORT_TIMEOUT_SECONDS="${OTLP_EXPORT_TIMEOUT_SECONDS}" \
  GOCACHE="${GOCACHE_DIR}" \
  GOMODCACHE="${GOMODCACHE_DIR}" \
  go run ./cmd/gateway >"${GATEWAY_LOG}" 2>&1 &
gateway_pid=$!
printf 'gateway pid=%s\n' "${gateway_pid}"
wait_for_http "${BASE_URL}/healthz" "gateway"

print_section "run observability demo check"
PROMETHEUS_URL="${PROMETHEUS_URL}" GRAFANA_URL="${GRAFANA_URL}" ./scripts/observability-demo-check.sh

print_section "observability demo verify passed"
printf 'base_url=%s\n' "${BASE_URL}"
printf 'prometheus_url=%s\n' "${PROMETHEUS_URL}"
printf 'grafana_url=%s\n' "${GRAFANA_URL}"
printf 'otlp_traces_endpoint=%s\n' "${OTLP_TRACES_ENDPOINT}"
