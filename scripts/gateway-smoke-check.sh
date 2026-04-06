#!/usr/bin/env bash

set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
API_KEY="${API_KEY:-lag-local-dev-key}"

print_section() {
  printf '\n== %s ==\n' "$1"
}

call_health() {
  print_section "GET /healthz"
  curl -i -sS "${BASE_URL}/healthz"
  printf '\n'

  print_section "GET /metrics"
  curl -sS "${BASE_URL}/metrics" | sed -n '1,40p'
  printf '\n'
}

call_models() {
  print_section "GET /v1/models"
  curl -i -sS "${BASE_URL}/v1/models" \
    -H "Authorization: Bearer ${API_KEY}"
  printf '\n'
}

call_usage() {
  print_section "GET /v1/usage"
  curl -i -sS "${BASE_URL}/v1/usage?limit=5" \
    -H "Authorization: Bearer ${API_KEY}"
  printf '\n'
}

call_chat() {
  print_section "POST /v1/chat/completions (non-stream)"
  curl -i -sS "${BASE_URL}/v1/chat/completions" \
    -H "Authorization: Bearer ${API_KEY}" \
    -H 'Content-Type: application/json' \
    -d '{"messages":[{"role":"user","content":"hello"}]}'
  printf '\n'
}

call_stream() {
  print_section "POST /v1/chat/completions (stream)"
  curl -i -sS -N "${BASE_URL}/v1/chat/completions" \
    -H "Authorization: Bearer ${API_KEY}" \
    -H 'Content-Type: application/json' \
    -d '{"messages":[{"role":"user","content":"hello"}],"stream":true}'
  printf '\n'
}

call_loadtest() {
  print_section "go run ./cmd/loadtest"
  go run ./cmd/loadtest -url "${BASE_URL}/v1/chat/completions" -auth-key "${API_KEY}" -requests 10 -concurrency 2
  printf '\n'
}

call_stream_loadtest() {
  print_section "go run ./cmd/loadtest -stream"
  go run ./cmd/loadtest -url "${BASE_URL}/v1/chat/completions" -auth-key "${API_KEY}" -requests 6 -concurrency 2 -stream
  printf '\n'
}

call_health
call_models
call_usage
call_chat
call_stream
call_loadtest
call_stream_loadtest
