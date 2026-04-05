#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-status}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
API_KEY="${API_KEY:-lag-local-dev-key}"

print_section() {
  printf '\n== %s ==\n' "$1"
}

call_status_endpoints() {
  print_section "GET /debug/providers"
  curl -sS "${BASE_URL}/debug/providers"
  printf '\n'

  print_section "GET /readyz"
  curl -i -sS "${BASE_URL}/readyz"
  printf '\n'
}

call_non_stream() {
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

case "${MODE}" in
  status)
    call_status_endpoints
    ;;
  create-fail)
    call_status_endpoints
    call_non_stream
    call_status_endpoints
    ;;
  stream-fail)
    call_status_endpoints
    call_stream
    call_status_endpoints
    ;;
  *)
    echo "usage: $0 [status|create-fail|stream-fail]" >&2
    exit 1
    ;;
esac
