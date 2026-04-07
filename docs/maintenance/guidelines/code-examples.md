# Code Example Guidelines

## Overview

Code examples in this repository must be runnable, recognizable, and tightly scoped to the behavior being documented.

## Commands

Use commands that match the repo’s actual entry points.

Prefer:

```bash
go run ./cmd/gateway
go run ./cmd/loadtest -auth-key lag-local-dev-key
./scripts/gateway-smoke-check.sh
```

Avoid invented wrappers or undocumented aliases.

## Local Defaults

When demonstrating local verification, prefer the repo’s established defaults:

- base URL: `http://127.0.0.1:8080`
- development key: `lag-local-dev-key`
- Compose file: `deployments/docker/docker-compose.yml`

If a different environment is required, say so explicitly.

## Response Examples

Response examples should preserve the fields that matter to the point being made.

For HTTP responses, keep:

- status line
- key headers when relevant
- the critical body markers

For streaming examples, always show:

- `Content-Type: text/event-stream`
- at least one `data:` chunk
- `data: [DONE]` when the stream completes normally

## Snippet Size

Keep snippets small enough to scan but large enough to verify.

Good:

- one request and one response
- one config block and one explanation
- one focused code excerpt

Bad:

- dumping an entire file when only three lines matter

## Pseudocode

Avoid pseudocode unless the exact syntax would distract from the architecture point. When pseudocode is used, label it clearly and do not mix it with real commands in the same block.
