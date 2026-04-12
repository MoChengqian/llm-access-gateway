#!/usr/bin/env bash

set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
API_KEY="${API_KEY:-lag-local-dev-key}"
ASSERT="${ASSERT:-false}"
HTTP_OK="HTTP/1.1 200 OK"

fail() {
  local message="$1"
  printf 'ERROR: %s\n' "${message}" >&2
  exit 1
  return 1
}

expect_contains() {
  local haystack="$1"
  local needle="$2"
  local label="$3"
  if [[ "${haystack}" != *"${needle}"* ]]; then
    fail "${label}: expected to find ${needle}"
  fi
  return 0
}

print_section() {
  local title="$1"
  printf '\n== %s ==\n' "${title}"
  return 0
}

call_health() {
  print_section "GET /healthz"
  local output
  output="$(curl -i -sS "${BASE_URL}/healthz")"
  printf '%s\n' "${output}"
  if [[ "${ASSERT}" == "true" ]]; then
    expect_contains "${output}" "${HTTP_OK}" "/healthz status"
    expect_contains "${output}" "X-Trace-Id:" "/healthz trace header"
  fi
  printf '\n'

  print_section "GET /metrics"
  output="$(curl -sS "${BASE_URL}/metrics")"
  printf '%s\n' "${output}" | sed -n '1,40p'
  if [[ "${ASSERT}" == "true" ]]; then
    expect_contains "${output}" "lag_http_requests_total" "/metrics request counter"
    expect_contains "${output}" "lag_http_request_duration_milliseconds_count" "/metrics request latency"
  fi
  printf '\n'
  return 0
}

call_models() {
  print_section "GET /v1/models"
  local output
  output="$(curl -i -sS "${BASE_URL}/v1/models" \
    -H "Authorization: Bearer ${API_KEY}")"
  printf '%s\n' "${output}"
  if [[ "${ASSERT}" == "true" ]]; then
    expect_contains "${output}" "${HTTP_OK}" "/v1/models status"
    expect_contains "${output}" "\"object\":\"list\"" "/v1/models object"
  fi
  printf '\n'
  return 0
}

call_usage() {
  print_section "GET /v1/usage"
  local output
  output="$(curl -i -sS "${BASE_URL}/v1/usage?limit=5" \
    -H "Authorization: Bearer ${API_KEY}")"
  printf '%s\n' "${output}"
  if [[ "${ASSERT}" == "true" ]]; then
    expect_contains "${output}" "${HTTP_OK}" "/v1/usage status"
    expect_contains "${output}" "\"object\":\"usage\"" "/v1/usage object"
    expect_contains "${output}" "\"summary\"" "/v1/usage summary"
  fi
  printf '\n'
  return 0
}

call_chat() {
  print_section "POST /v1/chat/completions (non-stream)"
  local output
  output="$(curl -i -sS "${BASE_URL}/v1/chat/completions" \
    -H "Authorization: Bearer ${API_KEY}" \
    -H 'Content-Type: application/json' \
    -d '{"messages":[{"role":"user","content":"hello"}]}')"
  printf '%s\n' "${output}"
  if [[ "${ASSERT}" == "true" ]]; then
    expect_contains "${output}" "${HTTP_OK}" "non-stream status"
    expect_contains "${output}" "\"object\":\"chat.completion\"" "non-stream object"
  fi
  printf '\n'
  return 0
}

call_stream() {
  print_section "POST /v1/chat/completions (stream)"
  local output
  output="$(curl -i -sS -N "${BASE_URL}/v1/chat/completions" \
    -H "Authorization: Bearer ${API_KEY}" \
    -H 'Content-Type: application/json' \
    -d '{"messages":[{"role":"user","content":"hello"}],"stream":true}')"
  printf '%s\n' "${output}"
  if [[ "${ASSERT}" == "true" ]]; then
    expect_contains "${output}" "${HTTP_OK}" "stream status"
    expect_contains "${output}" "Content-Type: text/event-stream" "stream content-type"
    expect_contains "${output}" "data: [DONE]" "stream done marker"
  fi
  printf '\n'
  return 0
}

call_loadtest() {
  print_section "go run ./cmd/loadtest"
  local output
  output="$(go run ./cmd/loadtest -url "${BASE_URL}/v1/chat/completions" -auth-key "${API_KEY}" -requests 10 -concurrency 2 -min-success-rate 1.0 -json)"
  printf '%s\n' "${output}"
  if [[ "${ASSERT}" == "true" ]]; then
    expect_contains "${output}" "\"failure\": 0" "loadtest failure count"
    expect_contains "${output}" "\"status_counts\"" "loadtest status counts"
  fi
  printf '\n'
  return 0
}

call_stream_loadtest() {
  print_section "go run ./cmd/loadtest -stream"
  local output
  output="$(go run ./cmd/loadtest -url "${BASE_URL}/v1/chat/completions" -auth-key "${API_KEY}" -requests 6 -concurrency 2 -stream -min-success-rate 1.0 -json)"
  printf '%s\n' "${output}"
  if [[ "${ASSERT}" == "true" ]]; then
    expect_contains "${output}" "\"stream\": true" "stream loadtest mode"
    expect_contains "${output}" "\"failure\": 0" "stream loadtest failure count"
  fi
  printf '\n'
  return 0
}

call_health
call_models
call_usage
call_chat
call_stream
call_loadtest
call_stream_loadtest
