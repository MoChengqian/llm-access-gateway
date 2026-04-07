# 006: Multi-Tenant Governance Is More Than Rate Limiting

## Overview

The easiest way to build a shared LLM service is to proxy requests upstream and hope teams behave well.

That works right up until one tenant overruns costs, another bursts traffic, and nobody can explain which key triggered what.

This repository takes the opposite approach: governance is a first-class layer, not an afterthought.

## The Core Model

The gateway resolves every authenticated request into a tenant-scoped principal.

That principal carries the information the rest of the request path needs:

- tenant identity
- API key identity
- rate limits
- token budget context

From there, governance happens before provider invocation. That ordering matters because it prevents work from being sent upstream before the request is known to be admissible.

## Three Kinds of Control

The current model combines three different controls.

### RPM

Requests per minute prevent noisy tenants from overwhelming the gateway with raw request volume.

### TPM

Tokens per minute control model usage more directly, which matters because one chat request can be much more expensive than another.

### Token budget

A long-term token budget provides a cost boundary beyond short rolling windows.

That combination is stronger than a single rate limit because it handles both burst pressure and cumulative spend.

## Why Redis Plus MySQL Fallback Is a Good Trade-Off

Redis is the fast path for limiter counters. It is the right default for operational rate limiting.

But a hard Redis dependency would make the whole gateway brittle, so the implementation keeps a MySQL-backed limiter path available as fallback.

That gives the system a more graceful failure mode:

- Redis healthy: fast counter updates
- Redis unhealthy: governance still works, just on a slower path

The important thing is not perfection. It is preserving the governance boundary even when a dependency degrades.

## Usage Tracking Completes the Story

Governance is not only about rejecting traffic. It is also about recording what happened.

The gateway tracks request usage in MySQL so operators can answer:

- how many tokens a tenant has consumed
- how many requests succeeded or failed
- what recent usage history looks like

That turns governance from a narrow admission check into a system that supports accountability and analysis.

## Security Matters Too

One of the quiet strengths of the design is that it does not store raw API keys.

The gateway hashes presented API keys with SHA-256 and looks up the hash in MySQL. That means a database leak is not equivalent to handing out working bearer tokens.

For a gateway, this kind of boring security decision is exactly the sort of thing that makes the system trustworthy.

## Governance as a Layer, Not a Feature Flag

What makes this implementation interesting is the placement.

Governance sits between authentication and provider invocation. That means:

- identity is established first
- limits are evaluated next
- only admissible traffic reaches the provider layer

That is a clean architectural choice and one of the reasons the codebase remains understandable even as more behavior gets added.

## Related Documentation

- [Governance Model](../architecture/governance.md)
- [Authentication](../api/authentication.md)
- [Request Flow](../architecture/request-flow.md)
- [Observability Design](../architecture/observability.md)
