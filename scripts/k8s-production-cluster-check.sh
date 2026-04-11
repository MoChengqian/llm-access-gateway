#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-local}"
TARGET="${2:-all}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PRODUCTION_OVERLAY="${REPO_ROOT}/deployments/k8s-overlays/production"
PRODUCTION_HPA_OVERLAY="${REPO_ROOT}/deployments/k8s-overlays/production-hpa"

print_section() {
  printf '\n== %s ==\n' "$1"
}

fail() {
  printf 'ERROR: %s\n' "$1" >&2
  exit 1
}

warn() {
  printf 'WARN: %s\n' "$1" >&2
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

overlay_dir() {
  case "$1" in
    production)
      printf '%s\n' "${PRODUCTION_OVERLAY}"
      ;;
    production-hpa)
      printf '%s\n' "${PRODUCTION_HPA_OVERLAY}"
      ;;
    *)
      fail "unknown overlay target: $1"
      ;;
  esac
}

selected_targets() {
  case "${TARGET}" in
    production)
      printf '%s\n' production
      ;;
    production-hpa)
      printf '%s\n' production-hpa
      ;;
    all)
      printf '%s\n%s\n' production production-hpa
      ;;
    *)
      fail "usage: $0 [local|server-dry-run|checklist] [production|production-hpa|all]"
      ;;
  esac
}

assert_render_contains() {
  local rendered_file="$1"
  local kind="$2"
  local name="$3"

  if ! grep -Eq "^kind: ${kind}$" "${rendered_file}" || ! grep -Eq "^  name: ${name}$" "${rendered_file}"; then
    fail "rendered overlay missing ${kind}/${name}"
  fi
}

render_overlay() {
  local target="$1"
  local dir
  local rendered_file
  dir="$(overlay_dir "${target}")"
  rendered_file="/tmp/lag-${target}-overlay.yaml"

  print_section "render ${target} overlay"
  kubectl kustomize "${dir}" >"${rendered_file}"
  test -s "${rendered_file}" || fail "rendered overlay is empty: ${rendered_file}"

  assert_render_contains "${rendered_file}" Namespace llm-access-gateway
  assert_render_contains "${rendered_file}" Deployment llm-access-gateway
  assert_render_contains "${rendered_file}" Service llm-access-gateway
  assert_render_contains "${rendered_file}" Ingress llm-access-gateway
  assert_render_contains "${rendered_file}" NetworkPolicy llm-access-gateway-boundary
  assert_render_contains "${rendered_file}" PodDisruptionBudget llm-access-gateway

  if [[ "${target}" == "production-hpa" ]]; then
    assert_render_contains "${rendered_file}" HorizontalPodAutoscaler llm-access-gateway
  fi

  printf 'rendered %s\n' "${rendered_file}"
}

require_cluster_api() {
  print_section "cluster api discovery"
  kubectl cluster-info >/dev/null
  kubectl api-resources --api-group=networking.k8s.io | grep -q '^networkpolicies' || fail "cluster does not expose networking.k8s.io NetworkPolicy"
  kubectl api-resources --api-group=policy | grep -q '^poddisruptionbudgets' || fail "cluster does not expose policy PodDisruptionBudget"
  kubectl api-resources --api-group=autoscaling | grep -q '^horizontalpodautoscalers' || fail "cluster does not expose autoscaling HorizontalPodAutoscaler"
}

check_hpa_metrics_api() {
  print_section "hpa metrics api"
  kubectl get --raw /apis/metrics.k8s.io/v1beta1 >/dev/null || fail "metrics.k8s.io is unavailable; install metrics-server before applying production-hpa"
  printf 'metrics.k8s.io available\n'
}

server_dry_run_overlay() {
  local target="$1"
  local dir
  dir="$(overlay_dir "${target}")"

  print_section "server-side dry-run ${target} overlay"
  kubectl apply --server-side --dry-run=server -k "${dir}" >/tmp/lag-${target}-server-dry-run.txt
  test -s "/tmp/lag-${target}-server-dry-run.txt" || fail "server-side dry-run produced no output for ${target}"
  printf 'server-side dry-run passed for %s\n' "${target}"
}

print_checklist() {
  cat <<'CHECKLIST'

== production cluster checklist ==
- Replace image registry/tag before apply.
- Replace ingress host and TLS secret before apply.
- Replace MySQL DSN, Redis password, and provider API keys before apply.
- Confirm the CNI plugin enforces NetworkPolicy; API acceptance alone is not enforcement.
- Confirm namespace labels match NetworkPolicy namespaceSelectors: ingress-nginx, monitoring, llm-access-gateway.
- Confirm MySQL, Redis, OTLP collector, and provider HTTPS egress are reachable after NetworkPolicy enforcement.
- Confirm metrics-server is installed before applying production-hpa.
- Run rollout and post-apply smoke checks after real apply.
CHECKLIST
}

run_local() {
  require_command kubectl
  kubectl version --client=true >/dev/null
  while IFS= read -r target; do
    render_overlay "${target}"
  done < <(selected_targets)
  print_checklist
}

run_server_dry_run() {
  require_command kubectl
  kubectl version --client=true >/dev/null
  require_cluster_api
  while IFS= read -r target; do
    render_overlay "${target}"
    server_dry_run_overlay "${target}"
    if [[ "${target}" == "production-hpa" ]]; then
      check_hpa_metrics_api
    fi
  done < <(selected_targets)
  print_checklist
}

case "${MODE}" in
  local)
    run_local
    ;;
  server-dry-run)
    run_server_dry_run
    ;;
  checklist)
    print_checklist
    ;;
  *)
    fail "usage: $0 [local|server-dry-run|checklist] [production|production-hpa|all]"
    ;;
esac
