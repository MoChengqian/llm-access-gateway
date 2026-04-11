# Kubernetes Production Overlay Verification

Date: 2026-04-11

## Purpose

Prove that the repository-owned production Kubernetes overlay is renderable and
covered by the Stage 7 static delivery contract.

## Commands

```bash
make k8s-production-render
./scripts/validate-deployments.rb
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
- `PodDisruptionBudget/llm-access-gateway`

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
- `PodDisruptionBudget` with `minAvailable: 1`

## Result

The production overlay render and deployment validator pass locally. The Stage 7
static contract includes the overlay assets so future delivery changes cannot
silently drop the production bundle.
