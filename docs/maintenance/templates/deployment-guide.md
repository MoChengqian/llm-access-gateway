# Deployment Guide Template

## Overview

Explain what deployment path this guide covers:

- Docker Compose
- Kubernetes
- local shell
- single-host production-like environment

State the expected audience and the main dependencies.

## Prerequisites

List concrete requirements:

- tools that must be installed
- environment access requirements
- secrets or DSNs the operator must provide
- expected working directory

## Deployment Assets

List the files used by this guide:

- manifests
- compose files
- config files
- bootstrap jobs

## Deployment Steps

Provide numbered, executable steps.

```bash
command here
```

For each step, explain:

- what it changes
- what success looks like
- what dependency it unlocks

## Verification

Use concrete post-deploy checks:

```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
curl -i http://127.0.0.1:8080/debug/providers
curl -i http://127.0.0.1:8080/metrics
```

Add smoke or drill commands when they exist in the repo.

## Troubleshooting

List the most likely failure modes and how to diagnose them:

- missing secret
- port conflict
- bootstrap job failed
- gateway alive but not ready

## Cleanup or Rollback

Document:

- how to stop the deployment
- how to delete or roll back the resources
- whether persistent state is removed

## Related Documentation

- [Configuration Reference](../../deployment/configuration.md)

Replace with the real related documents.

## Code References

- [`deployments/...`](../../../deployments/)
- [`cmd/gateway/main.go`](../../../cmd/gateway/main.go)

Replace with the exact files used by this guide.
