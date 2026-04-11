# Kubernetes Deployment

## Overview

The repository ships a baseline Kubernetes deployment in [`deployments/k8s/`](../../deployments/k8s). The manifests are intentionally small and focus on running the current gateway with environment variables, health probes, and a bootstrap job for schema and seed data.

The directory contains:

- `namespace.yaml`
- `configmap.yaml`
- `secret.example.yaml`
- `job.yaml`
- `deployment.yaml`
- `service.yaml`

These manifests are enough to explain the deployment model, but they assume you already have external MySQL and Redis services available to the cluster.

## Resource Layout

### Namespace

`namespace.yaml` creates:

```text
Namespace/llm-access-gateway
```

All other manifests target that namespace.

### ConfigMap

`configmap.yaml` defines non-secret `APP_*` configuration such as:

- `APP_SERVER_ADDRESS`
- `APP_LOG_LEVEL`
- `APP_OBSERVABILITY_SERVICE_NAME`
- `APP_OBSERVABILITY_OTLP_TRACES_ENDPOINT`
- `APP_OBSERVABILITY_OTLP_EXPORT_TIMEOUT_SECONDS`
- `APP_REDIS_ADDRESS`
- `APP_GATEWAY_DEFAULT_MODEL`
- `APP_GATEWAY_PROVIDER_FAILURE_THRESHOLD`
- `APP_GATEWAY_PROVIDER_COOLDOWN_SECONDS`
- mock failure toggles for the primary backend

### Secret

`secret.example.yaml` is an example `Secret` that currently carries:

- `APP_MYSQL_DSN`

Before applying it, replace the example DSN with a value that points to a reachable MySQL service in your own cluster or network.

### Bootstrap Job

`job.yaml` defines `Job/llm-access-gateway-devinit`, which runs:

```text
/app/devinit
```

This job exists so schema/bootstrap work happens before the gateway pod starts serving traffic.

### Deployment

`deployment.yaml` defines:

- `Deployment/llm-access-gateway`
- one replica
- container image `llm-access-gateway:latest`
- container port `8080`
- readiness probe on `/readyz`
- liveness probe on `/healthz`
- requests `100m CPU / 128Mi memory`
- limits `500m CPU / 256Mi memory`

### Service

`service.yaml` publishes:

- `Service/llm-access-gateway`
- type `ClusterIP`
- port `8080`

## Apply Order

Apply the manifests in this order:

```bash
kubectl apply -f deployments/k8s/namespace.yaml
kubectl apply -f deployments/k8s/configmap.yaml
kubectl apply -f deployments/k8s/secret.example.yaml
kubectl apply -f deployments/k8s/job.yaml
kubectl -n llm-access-gateway wait --for=condition=complete job/llm-access-gateway-devinit --timeout=120s
kubectl apply -f deployments/k8s/deployment.yaml
kubectl apply -f deployments/k8s/service.yaml
kubectl -n llm-access-gateway get pods,svc
```

The order matters because:

1. the namespace must exist first
2. the ConfigMap and Secret must exist before pods can mount env vars from them
3. `devinit` should complete before the Deployment starts taking traffic

## Probe Semantics

The deployment uses two different probes:

### Liveness probe

```text
GET /healthz
```

This checks whether the process is up and able to respond.

### Readiness probe

```text
GET /readyz
```

This checks whether at least one provider backend is considered healthy by the gateway. It is the correct signal to use for traffic admission and rollout readiness.

That distinction is wired by [`internal/api/handlers/health.go`](../../internal/api/handlers/health.go) and [`internal/provider/router/chat.go`](../../internal/provider/router/chat.go).

## Configuration Flow

The Deployment uses `envFrom` for both the ConfigMap and Secret:

```yaml
envFrom:
  - configMapRef:
      name: llm-access-gateway-config
  - secretRef:
      name: llm-access-gateway-secrets
```

At runtime, those `APP_*` variables override the defaults loaded from [`configs/config.yaml`](../../configs/config.yaml).

## Cluster Validation Notes

To structurally inspect the manifests without applying them, you can always parse the files locally and review the expanded objects. During documentation work, the manifest YAML was confirmed to parse into the expected top-level kinds:

- `Namespace`
- `ConfigMap`
- `Secret`
- `Job`
- `Deployment`
- `Service`

Client-side `kubectl apply --dry-run=client` still depends on the current kube-context in this environment, so full API recognition requires a Kubernetes client that can reach your cluster control plane.

For repository-level structural checks that do not depend on cluster access, use:

```bash
./scripts/validate-deployments.rb
./scripts/stage7-verify.sh static
```

The Stage 7 static contract wraps that validator and also checks Go tests, vet,
dashboard JSON syntax, and required delivery/drill/nightly assets. The
deployment validator itself confirms the manifest kinds, namespace wiring,
Deployment probes, Service port, bootstrap Job command, and the Compose expansion
model used in local delivery.

## Operational Checks After Apply

Once the resources are applied in a real cluster, use:

```bash
kubectl -n llm-access-gateway get pods
kubectl -n llm-access-gateway get job llm-access-gateway-devinit
kubectl -n llm-access-gateway describe deployment llm-access-gateway
kubectl -n llm-access-gateway logs deployment/llm-access-gateway
kubectl -n llm-access-gateway port-forward svc/llm-access-gateway 8080:8080
```

Then, from another shell:

```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
curl -i http://127.0.0.1:8080/debug/providers
```

## Troubleshooting

### `devinit` job does not complete

Check:

```bash
kubectl -n llm-access-gateway logs job/llm-access-gateway-devinit
kubectl -n llm-access-gateway describe job llm-access-gateway-devinit
```

Likely causes:

- `APP_MYSQL_DSN` is wrong
- the MySQL service is not reachable from the cluster
- the image is missing or cannot be pulled

### Deployment is running but not ready

Check:

```bash
kubectl -n llm-access-gateway describe pod -l app=llm-access-gateway
kubectl -n llm-access-gateway logs deployment/llm-access-gateway
```

Then inspect `/readyz` and `/debug/providers` through port-forwarding. The most common reason is provider cooldown or unreachable upstream configuration.

## Related Documentation

- [Docker Compose Deployment](docker-compose.md)
- [Configuration Reference](configuration.md)
- [Production Considerations](production-considerations.md)
- [Local Development](../local-development.md)

## Code References

- [`deployments/k8s/namespace.yaml`](../../deployments/k8s/namespace.yaml)
- [`deployments/k8s/configmap.yaml`](../../deployments/k8s/configmap.yaml)
- [`deployments/k8s/secret.example.yaml`](../../deployments/k8s/secret.example.yaml)
- [`deployments/k8s/job.yaml`](../../deployments/k8s/job.yaml)
- [`deployments/k8s/deployment.yaml`](../../deployments/k8s/deployment.yaml)
- [`deployments/k8s/service.yaml`](../../deployments/k8s/service.yaml)
- [`internal/api/handlers/health.go`](../../internal/api/handlers/health.go)
- [`internal/provider/router/chat.go`](../../internal/provider/router/chat.go)
