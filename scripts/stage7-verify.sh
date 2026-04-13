#!/usr/bin/env bash

set -euo pipefail

MODE="${1:-static}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

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

require_file() {
  local path="$1"
  if [[ ! -f "${path}" ]]; then
    fail "required Stage 7 asset missing: ${path}"
  fi
  return 0
}

run_static_contract() {
  cd "${REPO_ROOT}"

  run_static_go_environment
  run_static_go_tests
  run_static_go_vet
  run_static_deployment_validation
  run_static_dashboard_validation
  run_static_asset_inventory
  printf 'Stage 7 static contract passed\n'
  return 0
}

run_static_go_environment() {
  cd "${REPO_ROOT}"

  print_section "go environment"
  go version
  go env GOOS GOARCH GOCACHE GOMODCACHE
  return 0
}

run_static_go_tests() {
  cd "${REPO_ROOT}"

  print_section "go test ./..."
  go clean -testcache

  local package_path
  while IFS= read -r package_path; do
    printf 'testing %s\n' "${package_path}"
    go test -count=1 -parallel=1 "${package_path}"
  done < <(go list ./...)
  return 0
}

run_static_go_vet() {
  cd "${REPO_ROOT}"

  print_section "go vet ./..."
  go vet ./...
  return 0
}

run_static_deployment_validation() {
  cd "${REPO_ROOT}"

  print_section "deployment manifest validation"
  ./scripts/validate-deployments.rb
  return 0
}

run_static_dashboard_validation() {
  cd "${REPO_ROOT}"

  print_section "grafana dashboard JSON validation"
  ruby -rjson -e 'JSON.parse(File.read("deployments/grafana/dashboards/llm-access-gateway.json")); puts "dashboard json ok"'
  return 0
}

run_static_asset_inventory() {
  cd "${REPO_ROOT}"

  print_section "required Stage 7 assets"
  local required_assets=(
    "deployments/docker/docker-compose.yml"
    ".sonarcloud.properties"
    "sonar-project.properties"
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
    "deployments/k8s-overlays/production/networkpolicy.yaml"
    "deployments/k8s-overlays/production/poddisruptionbudget.yaml"
    "deployments/k8s-overlays/production-hpa/kustomization.yaml"
    "deployments/k8s-overlays/production-hpa/horizontalpodautoscaler.yaml"
    "cmd/loadtest/main.go"
    "scripts/k8s-production-cluster-check.sh"
    "scripts/gateway-smoke-check.sh"
    "scripts/provider-fallback-drill.sh"
    "scripts/anthropic-adapter-drill.sh"
    "scripts/ci-start-background.sh"
    "scripts/ci-stop-background.sh"
    "scripts/sonar-quality-gate-check.rb"
    "cmd/nightlycheck/main.go"
    "cmd/nightlyreport/main.go"
    ".github/workflows/runtime-ci.yml"
    ".github/workflows/nightly-verification.yml"
    ".github/nightly/benchmark-baseline.json"
    "docs/verification/README.md"
    "docs/verification/stage7-delivery-contract.md"
    "docs/verification/stage7-production-readiness.md"
    "docs/verification/sonar-quality-gate.md"
    "docs/verification/k8s-production-overlay.md"
    "docs/verification/k8s-production-cluster-checklist.md"
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
  return 0
}

run_runtime_contract() {
  cd "${REPO_ROOT}"

  print_section "runtime smoke and built-in load contract"
  ASSERT=true ./scripts/gateway-smoke-check.sh
  printf 'Stage 7 runtime contract passed\n'
  return 0
}

case "${MODE}" in
  static)
    run_static_contract
    ;;
  static-go-env)
    run_static_go_environment
    ;;
  static-go-test)
    run_static_go_tests
    ;;
  static-go-vet)
    run_static_go_vet
    ;;
  static-deployments)
    run_static_deployment_validation
    ;;
  static-dashboard)
    run_static_dashboard_validation
    ;;
  static-assets)
    run_static_asset_inventory
    ;;
  runtime)
    run_runtime_contract
    ;;
  all)
    run_static_contract
    run_runtime_contract
    ;;
  *)
    fail "usage: $0 [static|static-go-env|static-go-test|static-go-vet|static-deployments|static-dashboard|static-assets|runtime|all]"
    ;;
esac
