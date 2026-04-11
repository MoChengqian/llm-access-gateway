# Architecture Decision Template

## Overview

State the design decision in two or three sentences:

- what problem was being solved
- what the chosen design is
- what important boundary or trade-off exists

## Context

Document the technical context that forced the decision:

- current system scope
- operational or protocol constraints
- upstream or downstream dependencies
- non-goals that shaped the design

## Decision

Describe the chosen design in concrete terms:

- main components
- control flow
- data flow
- failure behavior

If helpful, include a small diagram:

```text
Caller -> Middleware -> Service -> Provider
```

## Why This Design

List the reasons the design fits the current codebase:

- simplicity
- compatibility
- observability
- safety
- testability

## Trade-Offs and Limits

Be explicit about what the design does not solve.

Examples:

- ordered failover instead of weighted routing
- in-process state instead of shared distributed state
- optional OTLP export while tracing storage remains environment-owned

## Verification

Tie the decision to evidence:

- unit or integration tests
- reproducible curl commands
- failure drills
- metrics or logs

## Related Documentation

- [Architecture Overview](../../architecture/overview.md)

Replace this list with the exact companion documents.

## Code References

- [`internal/...`](../../../internal/)

Replace these entries with the exact files that embody the decision.
