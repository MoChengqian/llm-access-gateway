# Kubernetes Production Cluster Checklist

Date: 2026-04-13

## Purpose

Standardize the last verification gate before applying the production
Kubernetes overlays to a real cluster.

Local render checks prove that manifests compose correctly. They do not prove
that a target cluster accepts the objects, has metrics for HPA, or enforces
NetworkPolicy. This checklist closes that gap without making CI depend on a
live cluster.

This page is only applicable once a real cluster or staging cluster exists. If
you are validating the repository without any target environment yet, stop at
the local check and treat the server-side dry-run as deferred rollout guidance.

## Applicability

Use this page in two phases:

- repository-only phase: run `make k8s-production-local-check`
- environment rollout phase: run `make k8s-production-server-dry-run` after
  you have a real cluster, valid kubeconfig, and environment-owned values

## Local Check

Run this without a cluster:

```bash
make k8s-production-local-check
./scripts/k8s-production-cluster-check.sh local all
```

This renders both overlays and verifies the expected objects are present:

- `Deployment/llm-access-gateway`
- `Service/llm-access-gateway`
- `Ingress/llm-access-gateway`
- `NetworkPolicy/llm-access-gateway-boundary`
- `PodDisruptionBudget/llm-access-gateway`
- `HorizontalPodAutoscaler/llm-access-gateway` in the optional HPA overlay

## Server-Side Dry Run

Run this against the chosen target cluster before the first apply:

```bash
make k8s-production-server-dry-run
./scripts/k8s-production-cluster-check.sh server-dry-run all
```

This checks:

- cluster API discovery is reachable
- `networking.k8s.io` exposes `NetworkPolicy`
- `policy` exposes `PodDisruptionBudget`
- `autoscaling` exposes `HorizontalPodAutoscaler`
- `kubectl apply --server-side --dry-run=server -k deployments/k8s-overlays/production` succeeds
- `kubectl apply --server-side --dry-run=server -k deployments/k8s-overlays/production-hpa` succeeds
- `metrics.k8s.io` is available before applying the HPA overlay

Before trusting any dry-run result, verify the cluster access baseline is still
valid:

- the current `kubectl` context points at the intended cluster
- kubeconfig CA data still matches the current apiserver trust chain
- kubeconfig user credentials are still accepted by the apiserver

The cluster-check script now prints the active context and server first, then
turns common TLS and credential failures into explicit remediation guidance
instead of surfacing only a raw `kubectl` exit code.

## Historical Environment Attempt

The latest recorded environment-access failure is retained in
[`k8s-production-server-dry-run-2026-04-12.md`](k8s-production-server-dry-run-2026-04-12.md).

That note is useful as a troubleshooting example, not as an open Stage 7
repository blocker. It proved the overlays still rendered locally, but the
operator workstation's kubeconfig had drifted from the target cluster:

- kubeconfig CA data no longer matched the apiserver certificate chain
- the embedded `kubernetes-admin` client credentials were rejected by the
  apiserver even when TLS verification was bypassed
- SSH access from the workstation to the control-plane node was not available,
  so `/etc/kubernetes/admin.conf` could not be refreshed in-place

Treat this as an environment-owned blocker. The Stage 7 repository contract can
still be complete without fresh cluster evidence; only cluster rollout claims
must wait until valid Kubernetes admin access exists again.

## Manual Cluster Checks

Before real apply, confirm:

- image registry and tag are environment-owned values
- ingress host and TLS secret are environment-owned values
- MySQL DSN, Redis password, and provider API keys are replaced
- the CNI plugin actually enforces NetworkPolicy
- namespace labels match the NetworkPolicy assumptions: `ingress-nginx`, `monitoring`, and `llm-access-gateway`
- MySQL, Redis, OTLP collector, and provider HTTPS egress remain reachable after policy enforcement
- metrics-server is installed before applying `production-hpa`

## Post-Apply Checks

After applying the production overlay:

```bash
kubectl -n llm-access-gateway rollout status deployment/llm-access-gateway --timeout=180s
kubectl -n llm-access-gateway get deploy,svc,ingress,networkpolicy,pdb
kubectl -n llm-access-gateway logs deployment/llm-access-gateway
```

If applying the optional HPA overlay:

```bash
kubectl -n llm-access-gateway get hpa llm-access-gateway
kubectl top pods -n llm-access-gateway
```

Then run the gateway smoke checks through the target ingress or a temporary
port-forward.

For the full Stage 7 readiness matrix, see
[`stage7-production-readiness.md`](stage7-production-readiness.md).
