#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-static}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

print_section() {
  printf '\n== %s ==\n' "$1"
}

fail() {
  printf 'ERROR: %s\n' "$1" >&2
  exit 1
}

require_file() {
  local path="$1"
  if [[ ! -f "${path}" ]]; then
    fail "required Stage 7 asset missing: ${path}"
  fi
}

run_static_contract() {
  cd "${REPO_ROOT}"

  print_section "go test ./..."
  go test ./...

  print_section "go vet ./..."
  go vet ./...

  print_section "deployment manifest validation"
  ./scripts/validate-deployments.rb

  print_section "grafana dashboard JSON validation"
  ruby -rjson -e 'JSON.parse(File.read("deployments/grafana/dashboards/llm-access-gateway.json")); puts "dashboard json ok"'

  print_section "required Stage 7 assets"
  local required_assets=(
    "deployments/docker/docker-compose.yml"
    "deployments/k8s/namespace.yaml"
    "deployments/k8s/kustomization.yaml"
    "deployments/k8s/configmap.yaml"
    "deployments/k8s/secret.example.yaml"
    "deployments/k8s/job.yaml"
    "deployments/k8s/deployment.yaml"
    "deployments/k8s/service.yaml"
    "deployments/k8s-overlays/production/kustomization.yaml"
    "deployments/k8s-overlays/production/configmap.patch.yaml"
    "deployments/k8s-overlays/production/secret.patch.yaml"
    "deployments/k8s-overlays/production/deployment.patch.yaml"
    "deployments/k8s-overlays/production/job.patch.yaml"
    "deployments/k8s-overlays/production/ingress.yaml"
    "deployments/k8s-overlays/production/poddisruptionbudget.yaml"
    "cmd/loadtest/main.go"
    "scripts/gateway-smoke-check.sh"
    "scripts/provider-fallback-drill.sh"
    "scripts/anthropic-adapter-drill.sh"
    "cmd/nightlycheck/main.go"
    "cmd/nightlyreport/main.go"
    ".github/workflows/runtime-ci.yml"
    ".github/workflows/nightly-verification.yml"
    ".github/nightly/benchmark-baseline.json"
    "docs/verification/stage7-delivery-contract.md"
    "docs/verification/k8s-production-overlay.md"
    "docs/verification/benchmarks/methodology.md"
    "docs/verification/failure-drills/anthropic-adapter.md"
    "docs/verification/failure-drills/provider-errors.md"
    "docs/verification/failure-drills/provider-timeout.md"
    "docs/verification/failure-drills/quota-enforcement.md"
    "docs/verification/failure-drills/streaming-failures.md"
  )
  for asset in "${required_assets[@]}"; do
    require_file "${asset}"
  done
  printf 'Stage 7 static contract passed\n'
}

run_runtime_contract() {
  cd "${REPO_ROOT}"

  print_section "runtime smoke and built-in load contract"
  ASSERT=true ./scripts/gateway-smoke-check.sh
  printf 'Stage 7 runtime contract passed\n'
}

case "${MODE}" in
  static)
    run_static_contract
    ;;
  runtime)
    run_runtime_contract
    ;;
  all)
    run_static_contract
    run_runtime_contract
    ;;
  *)
    fail "usage: $0 [static|runtime|all]"
    ;;
esac
