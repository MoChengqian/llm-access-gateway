#!/usr/bin/env bash

set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
STUB_ADDRESS="${STUB_ADDRESS:-127.0.0.1:4318}"
STUB_PATH="${STUB_PATH:-/v1/traces}"
CAPTURE_FILE="${CAPTURE_FILE:-/tmp/lag-otlpstub-capture.json}"
STUB_LOG="${STUB_LOG:-/tmp/lag-otlpstub.log}"
TRIGGER_PATH="${TRIGGER_PATH:-/healthz}"
START_STUB="${START_STUB:-true}"
WAIT_SECONDS="${WAIT_SECONDS:-15}"

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

cleanup() {
  if [[ -n "${stub_pid:-}" ]]; then
    kill "${stub_pid}" >/dev/null 2>&1 || true
    wait "${stub_pid}" 2>/dev/null || true
  fi
}

trap cleanup EXIT

rm -f "${CAPTURE_FILE}"

if [[ "${START_STUB}" == "true" ]]; then
  print_section "start OTLP stub"
  nohup go run ./cmd/otlpstub -address "${STUB_ADDRESS}" -path "${STUB_PATH}" -output "${CAPTURE_FILE}" >"${STUB_LOG}" 2>&1 &
  stub_pid=$!
  wait_for_http "http://${STUB_ADDRESS}/healthz" "OTLP stub"
  printf 'stub pid=%s\n' "${stub_pid}"
fi

print_section "check gateway"
wait_for_http "${BASE_URL}/healthz" "gateway"
printf 'gateway ready at %s\n' "${BASE_URL}"

print_section "trigger traced request"
curl -fsS "${BASE_URL}${TRIGGER_PATH}" >/dev/null
printf 'triggered %s%s\n' "${BASE_URL}" "${TRIGGER_PATH}"

print_section "wait for OTLP export"
i=0
for i in $(seq 1 "${WAIT_SECONDS}"); do
  if [[ -f "${CAPTURE_FILE}" ]] && grep -q '"request_count":[[:space:]]*[1-9]' "${CAPTURE_FILE}"; then
    cat "${CAPTURE_FILE}"
    exit 0
  fi
  sleep 1
done

if [[ -f "${STUB_LOG}" ]]; then
  printf '\n-- stub log --\n'
  cat "${STUB_LOG}"
fi
fail "gateway did not export OTLP traces to ${STUB_ADDRESS}${STUB_PATH}; ensure it is running with APP_OBSERVABILITY_OTLP_TRACES_ENDPOINT=http://${STUB_ADDRESS}${STUB_PATH}"
