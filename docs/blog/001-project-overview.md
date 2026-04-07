# 001: Why Build an LLM Access Gateway?

## Overview

Most teams do not need to train models. They need to control how applications reach models that already exist.

That is the problem this repository tackles.

LLM Access Gateway sits between client applications and upstream model providers and focuses on the part that becomes painful as soon as more than one team, key, or provider is involved:

- one API surface for clients
- tenant-aware authentication
- request and token governance
- streaming proxy behavior
- provider fallback and health handling
- enough observability to debug failures

It is a gateway layer, not a model platform.

## What It Does

The current implementation exposes OpenAI-compatible routes:

- `POST /v1/chat/completions`
- `GET /v1/models`
- `GET /v1/usage`

Around those routes, it adds operational behavior that real teams usually need:

- API key authentication backed by MySQL
- per-tenant RPM, TPM, and token budget enforcement
- SSE streaming passthrough
- primary/secondary provider failover
- request and trace correlation
- health, readiness, debug, and metrics endpoints

That combination makes the project a good example of infrastructure glue code: not flashy, but full of the decisions that determine whether a service is trustworthy in production.

## What It Does Not Do

The boundaries matter just as much as the features.

This project does not try to be:

- a training system
- an inference acceleration layer
- a RAG platform
- a vector database
- a frontend dashboard
- an agent orchestration framework

The narrow scope is a strength. It keeps the design centered on access, governance, routing, and operational clarity.

## Why This Shape Makes Sense

A gateway earns its keep when it absorbs cross-cutting concerns so application teams do not have to reimplement them in every service.

In this repository, that shows up in a few design choices:

- handlers stay thin and pass business logic into services
- auth resolves a tenant-scoped principal before business logic runs
- governance happens before provider invocation
- streaming fallback is only allowed before the first chunk
- provider health influences readiness and failover
- request IDs and trace IDs are preserved across the full request path

None of those decisions are individually exotic. Together, they make the system coherent.

## Who This Project Is For

This repository is useful to three kinds of readers:

### Engineers

If you want to understand or extend a gateway, the code shows a clean separation between:

- HTTP handling
- authentication
- governance
- chat service logic
- provider routing
- observability

### Interviewers

If you are evaluating engineering depth, the interesting parts are not just the endpoints. They are the constraints and trade-offs:

- what fallback means for streams
- why readiness is different from liveness
- how rate limiting degrades when Redis is unavailable
- why request correlation matters operationally

### Learners

If you are studying backend systems, this project is a compact example of how to turn a “simple proxy” into a service with real governance and operational behavior.

## Where to Go Next

If you want the fast path through the docs:

1. [Quick Start Guide](../quick-start-guide.md)
2. [Architecture Overview](../architecture/overview.md)
3. [Routing and Resilience](../architecture/routing-resilience.md)
4. [Observability Design](../architecture/observability.md)
5. [Local Development](../local-development.md)

If you want implementation detail:

- [API Endpoints](../api/endpoints.md)
- [Provider Adapter Design](../architecture/provider-adapters.md)
- [Streaming Proxy Architecture](../architecture/streaming-proxy.md)
- [Governance Model](../architecture/governance.md)

## Related Documentation

- [Quick Start Guide](../quick-start-guide.md)
- [Architecture Overview](../architecture/overview.md)
- [API Reference](../api.md)
- [Documentation Index](../README.md)
