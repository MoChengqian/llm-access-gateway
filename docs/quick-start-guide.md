# Quick Start Guide for Interviewers

**Reading time: about 10 minutes**

This is the shortest project-level introduction for interviewers, reviewers,
and hiring managers. It is not the full documentation set. It is the minimum
path to understand what the repository is trying to prove and where the real
evidence lives.

## Core Judgment

LLM Access Gateway is a Go-based multi-tenant model access and governance
gateway. It is strongest as infrastructure evidence in three areas:

- Stage 5: MySQL-backed routing policy, quota governance, and deterministic
  fallback
- Stage 6: request and trace correlation, metrics, and locally verifiable OTLP
  export
- Stage 7: delivery, load, drill, and verification assets that turn the
  repository into something runnable and reviewable

## What This Project Is

This repository sits between client applications and upstream LLM providers. It
keeps the external API OpenAI-compatible while making these concerns explicit:

- who can call the gateway
- which tenant a request belongs to
- whether quota and budget rules admit the request
- which backend should handle the request
- how fallback behaves when providers fail
- how the request is observed and verified

The practical scope is gateway engineering, not AI application assembly.

## What This Project Is Not

This repository is intentionally not:

- a model training or inference optimization system
- a RAG or vector retrieval stack
- a frontend chat UI
- an agent workflow platform
- a generic AI demo collection

That boundary is part of the project quality. The repository stays focused on
model access, governance, resilience, observability, and delivery.

## What Is Worth Evaluating

If you only remember four things about this project, these are the right four:

### 1. Routing Policy Is Persisted, Not Just Hard-Coded

The gateway builds backend definitions from process config, but request routing
can be driven by persisted MySQL `route_rules`. That means backend choice is no
longer only a static config concern.

Why it matters:

- model-specific routing is explicit
- generic fallback remains explicit
- operator workflow exists through `cmd/routerulectl`

Start here:

- [Routing & Resilience](architecture/routing-resilience.md)
- [Verification Index](verification/README.md)

### 2. Streaming Fallback Has A Real Boundary

The gateway allows fallback only before the first successful stream chunk is
accepted from the upstream provider. After that point, it can close the stream
on interruption, but it cannot switch providers without corrupting the client
response.

Why it matters:

- the project does not hide a hard streaming constraint behind vague wording
- resilience behavior is bounded and testable
- the implementation matches the documented contract

Start here:

- [Streaming Proxy](architecture/streaming-proxy.md)
- [Streaming Failure Drill](verification/failure-drills/streaming-failures.md)

### 3. Observability Is Lightweight But Verifiable

The repository does not pretend to ship a full production observability
platform. What it does ship is a real request-correlation contract:

- `X-Request-Id`
- `X-Trace-Id`
- structured logs
- Prometheus-style metrics
- optional OTLP/HTTP export
- local collector, Prometheus, and Grafana verification assets

Why it matters:

- the observability story is concrete
- local evidence exists without claiming ownership of every production backend
- debugging signals are part of the system contract, not an afterthought

Start here:

- [Observability Design](architecture/observability.md)
- [OTLP Export Verification](verification/otlp-export.md)
- [Observability Demo Runtime](verification/observability-demo-runtime.md)

### 4. The Repository Has A Verification Contract

The strongest Stage 7 signal is not “there are scripts somewhere.” It is that
the repository defines primary verification entrypoints for:

- static contract checks
- runtime smoke and built-in load checks
- failure drills
- Kubernetes render checks
- SonarCloud release-gate checks

Why it matters:

- code, deployment assets, and evidence are tied together
- nightly checks protect the same contract that local engineers use
- repository completion is separated from environment-owned rollout acceptance

Start here:

- [Verification Index](verification/README.md)
- [Stage 7 Delivery Contract](verification/stage7-delivery-contract.md)
- [Stage 7 Production Readiness](verification/stage7-production-readiness.md)

## Fast Evidence Path

If you want to verify the repository rather than only read about it, these are
the highest-signal entrypoints:

```bash
make stage7-static
make stage7-runtime
make observability-demo-verify
make k8s-production-local-check
make sonar-quality-gate-check
```

These commands prove different layers of the project:

- static correctness and committed assets
- local request-path behavior
- observability loop validity
- Kubernetes delivery renderability
- external quality-gate status

The environment-only cluster gate is separate:

```bash
make k8s-production-server-dry-run
```

Run that only when a real cluster exists. The repository does not claim cluster
readiness by default.

## If You Only Have 10 Minutes

Read these in order:

1. this guide
2. [Architecture Overview](architecture/overview.md)
3. [Verification Index](verification/README.md)

That set is the shortest honest path to:

- project position
- system boundary
- main engineering decisions
- proof and verification trail

## If You Want To Dig One Layer Deeper

- project boundary and staged completion:
  [Execution Roadmap](execution-roadmap.md)
- local run path:
  [Local Development](local-development.md)
- endpoint behavior:
  [API Reference](api.md)
- deployment surface:
  [README.md](../README.md)

## Bottom Line

This repository is worth evaluating as infrastructure work, not as an AI
application demo. Its strongest signal is that routing, fallback,
observability, and delivery are implemented with explicit boundaries and backed
by runnable evidence.

**Last Updated**: 2026-04-13
**Audience**: Interviewers, recruiters, and technical reviewers
