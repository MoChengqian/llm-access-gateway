#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-local}"
TARGET="${2:-all}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PRODUCTION_OVERLAY="${REPO_ROOT}/deployments/k8s-overlays/production"
PRODUCTION_HPA_OVERLAY="${REPO_ROOT}/deployments/k8s-overlays/production-hpa"

print_section() {
  local section_title="$1"
  printf '\n== %s ==\n' "${section_title}"
  return 0
}

fail() {
  local message="$1"
  printf 'ERROR: %s\n' "${message}" >&2
  exit 1
  return 1
}

warn() {
  local message="$1"
  printf 'WARN: %s\n' "${message}" >&2
  return 0
}

current_context() {
  kubectl config current-context 2>/dev/null || true
  return 0
}

current_cluster_server() {
  kubectl config view --raw --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null || true
  return 0
}

diagnose_cluster_access_failure() {
  local command_name="$1"
  local log_file="$2"
  local context_name
  local cluster_server
  context_name="$(current_context)"
  cluster_server="$(current_cluster_server)"

  warn "${command_name} failed for context=${context_name:-unknown} server=${cluster_server:-unknown}"

  if grep -Eq 'certificate signed by unknown authority|failed to verify certificate|x509:' "${log_file}"; then
    fail "cluster TLS trust failed; refresh kubeconfig certificate-authority-data or replace kubeconfig from the control-plane admin.conf, then rerun server-dry-run"
  fi

  if grep -Eq 'Unauthorized|provide credentials|You must be logged in to the server' "${log_file}"; then
    fail "cluster credentials were rejected; refresh the kubeconfig user credentials or replace kubeconfig from the control-plane admin.conf, then rerun server-dry-run"
  fi

  fail "${command_name} failed; inspect ${log_file} for details"
  return 1
}

require_command() {
  local command_name="$1"
  command -v "${command_name}" >/dev/null 2>&1 || fail "${command_name} is required"
  return 0
}

overlay_dir() {
  local target="$1"
  case "${target}" in
    production)
      printf '%s\n' "${PRODUCTION_OVERLAY}"
      ;;
    production-hpa)
      printf '%s\n' "${PRODUCTION_HPA_OVERLAY}"
      ;;
    *)
      fail "unknown overlay target: ${target}"
      ;;
  esac
  return 0
}

selected_targets() {
  local selected_target="${TARGET}"
  case "${selected_target}" in
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
  return 0
}

assert_render_contains() {
  local rendered_file="$1"
  local kind="$2"
  local name="$3"

  if ! grep -Eq "^kind: ${kind}$" "${rendered_file}" || ! grep -Eq "^  name: ${name}$" "${rendered_file}"; then
    fail "rendered overlay missing ${kind}/${name}"
  fi
  return 0
}

render_overlay() {
  local target="$1"
  local dir
  local rendered_file
  dir="$(overlay_dir "${target}")"
  rendered_file="/tmp/lag-${target}-overlay.yaml"

  print_section "render ${target} overlay"
  kubectl kustomize "${dir}" >"${rendered_file}"
  [[ -s "${rendered_file}" ]] || fail "rendered overlay is empty: ${rendered_file}"

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
  return 0
}

require_cluster_api() {
  local cluster_info_log="/tmp/lag-k8s-cluster-info.log"

  print_section "cluster api discovery"
  printf 'context=%s\n' "$(current_context)"
  printf 'server=%s\n' "$(current_cluster_server)"
  if ! kubectl cluster-info >"${cluster_info_log}" 2>&1; then
    cat "${cluster_info_log}" >&2
    diagnose_cluster_access_failure "kubectl cluster-info" "${cluster_info_log}"
  fi
  kubectl api-resources --api-group=networking.k8s.io | grep -q '^networkpolicies' || fail "cluster does not expose networking.k8s.io NetworkPolicy"
  kubectl api-resources --api-group=policy | grep -q '^poddisruptionbudgets' || fail "cluster does not expose policy PodDisruptionBudget"
  kubectl api-resources --api-group=autoscaling | grep -q '^horizontalpodautoscalers' || fail "cluster does not expose autoscaling HorizontalPodAutoscaler"
  return 0
}

check_hpa_metrics_api() {
  print_section "hpa metrics api"
  kubectl get --raw /apis/metrics.k8s.io/v1beta1 >/dev/null || fail "metrics.k8s.io is unavailable; install metrics-server before applying production-hpa"
  printf 'metrics.k8s.io available\n'
  return 0
}

server_dry_run_overlay() {
  local target="$1"
  local dir
  dir="$(overlay_dir "${target}")"

  print_section "server-side dry-run ${target} overlay"
  kubectl apply --server-side --dry-run=server -k "${dir}" >/tmp/lag-${target}-server-dry-run.txt
  [[ -s "/tmp/lag-${target}-server-dry-run.txt" ]] || fail "server-side dry-run produced no output for ${target}"
  printf 'server-side dry-run passed for %s\n' "${target}"
  return 0
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
  return 0
}

run_local() {
  require_command kubectl
  kubectl version --client=true >/dev/null
  while IFS= read -r target; do
    render_overlay "${target}"
  done < <(selected_targets)
  print_checklist
  return 0
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
  return 0
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
