#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-status}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
UPSTREAM_BASE_URL="${UPSTREAM_BASE_URL:-http://127.0.0.1:18081}"
API_KEY="${API_KEY:-lag-local-dev-key}"
MODEL="${MODEL:-claude-3-5-sonnet-latest}"

print_section() {
  printf '\n== %s ==\n' "$1"
}

call_system_prompt() {
  print_section "POST /v1/chat/completions (system prompt translation)"
  curl -i -sS "${BASE_URL}/v1/chat/completions" \
    -H "Authorization: Bearer ${API_KEY}" \
    -H 'Content-Type: application/json' \
    -d "$(cat <<JSON
{"model":"${MODEL}","messages":[{"role":"system","content":"Be concise."},{"role":"user","content":"reply in five words"},{"role":"system","content":"Use JSON only."}]}
JSON
)"
  printf '\n'

  print_section "GET synthetic upstream /debug/last-request"
  curl -sS "${UPSTREAM_BASE_URL}/debug/last-request"
  printf '\n'
}

call_partial_stream() {
  print_section "POST /v1/chat/completions (anthropic partial stream)"
  curl -i -sS -N "${BASE_URL}/v1/chat/completions" \
    -H "Authorization: Bearer ${API_KEY}" \
    -H 'Content-Type: application/json' \
    -d "$(cat <<JSON
{"model":"${MODEL}","messages":[{"role":"user","content":"hello"}],"stream":true}
JSON
)"
  printf '\n'
}

case "${MODE}" in
  status)
    print_section "GET synthetic upstream /debug/last-request"
    curl -sS "${UPSTREAM_BASE_URL}/debug/last-request"
    printf '\n'
    ;;
  system-prompt)
    call_system_prompt
    ;;
  partial-stream)
    call_partial_stream
    ;;
  *)
    echo "usage: $0 [status|system-prompt|partial-stream]" >&2
    exit 1
    ;;
esac
