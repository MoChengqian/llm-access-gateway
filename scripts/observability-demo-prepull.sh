#!/usr/bin/env bash

set -euo pipefail

OTEL_COLLECTOR_IMAGE="${OTEL_COLLECTOR_IMAGE:-otel/opentelemetry-collector-contrib:0.143.0}"
PROMETHEUS_IMAGE="${PROMETHEUS_IMAGE:-prom/prometheus:v3.9.1}"
GRAFANA_IMAGE="${GRAFANA_IMAGE:-grafana/grafana:12.3.1}"
FORCE_PULL="${FORCE_PULL:-false}"
PULL_RETRIES="${PULL_RETRIES:-3}"
PULL_BACKOFF_SECONDS="${PULL_BACKOFF_SECONDS:-5}"

print_section() {
  local section_title="$1"
  printf '\n== %s ==\n' "${section_title}"
  return 0
}

fail() {
  local message="$1"
  printf 'ERROR: %s\n' "${message}" >&2
  exit 1
}

pull_with_retry() {
  local image="$1"
  local attempt=0

  if [[ "${FORCE_PULL}" != "true" ]] && docker image inspect "${image}" >/dev/null 2>&1; then
    printf 'cached image: %s\n' "${image}"
    return 0
  fi

  for attempt in $(seq 1 "${PULL_RETRIES}"); do
    if docker pull "${image}"; then
      printf 'pulled image: %s\n' "${image}"
      return 0
    fi
    if [[ "${attempt}" -lt "${PULL_RETRIES}" ]]; then
      printf 'retrying %s in %ss (%s/%s)\n' "${image}" "${PULL_BACKOFF_SECONDS}" "${attempt}" "${PULL_RETRIES}"
      sleep "${PULL_BACKOFF_SECONDS}"
    fi
  done

  fail "failed to pull ${image} after ${PULL_RETRIES} attempts"
}

print_section "prepare observability demo images"
pull_with_retry "${OTEL_COLLECTOR_IMAGE}"
pull_with_retry "${PROMETHEUS_IMAGE}"
pull_with_retry "${GRAFANA_IMAGE}"

print_section "local image summary"
docker image inspect \
  --format '{{join .RepoTags ", "}} {{.Id}} {{.Size}}' \
  "${OTEL_COLLECTOR_IMAGE}" \
  "${PROMETHEUS_IMAGE}" \
  "${GRAFANA_IMAGE}"
