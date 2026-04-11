# Kubernetes Production Overlay Verification

Date: 2026-04-11

## Purpose

Prove that the repository-owned production Kubernetes overlay is renderable and
covered by the Stage 7 static delivery contract.

## Commands

```bash
make k8s-production-render
make k8s-production-hpa-render
./scripts/validate-deployments.rb
REQUIRE_K8S_PRODUCTION_RENDER=true ./scripts/validate-deployments.rb
GOCACHE=/tmp/lag-project-gocache GOMODCACHE=/tmp/lag-project-gomodcache ./scripts/stage7-verify.sh static
```

## Verified Contract

`make k8s-production-render` renders:

- `Namespace/llm-access-gateway`
- `ConfigMap/llm-access-gateway-config`
- `Secret/llm-access-gateway-secrets`
- `Service/llm-access-gateway`
- `Deployment/llm-access-gateway`
- `Job/llm-access-gateway-devinit`
- `Ingress/llm-access-gateway`
- `NetworkPolicy/llm-access-gateway-boundary`
- `PodDisruptionBudget/llm-access-gateway`

`make k8s-production-hpa-render` renders the same production bundle plus:

- `HorizontalPodAutoscaler/llm-access-gateway`

`scripts/validate-deployments.rb` validates the base manifests plus the
production overlay source files. When `kubectl` is available, it also renders
the overlay and checks the final object contract:

- non-`latest` gateway image override
- OpenAI primary and Anthropic secondary provider config
- Redis and OTLP service wiring
- Secret placeholders for MySQL, Redis, and provider API keys
- two gateway replicas
- rolling update with `maxUnavailable: 0`
- Prometheus scrape annotations
- pod and container security defaults
- bootstrap Job TTL and resources
- nginx Ingress host/TLS/backend wiring
- `NetworkPolicy` namespace and egress-port wiring
- `PodDisruptionBudget` with `minAvailable: 1`
- optional `HorizontalPodAutoscaler` with 2-6 replicas and 70% CPU target

CI sets `REQUIRE_K8S_PRODUCTION_RENDER=true` and runs
`make k8s-production-render` plus `make k8s-production-hpa-render` before
`./scripts/stage7-verify.sh static`. This prevents a runner without `kubectl`
from silently skipping the render check.

## Result

The production overlay render and deployment validator pass locally. The Stage 7
static contract includes the overlay assets, and CI now requires the render path
explicitly, so future delivery changes cannot silently drop or break the
production bundle.
