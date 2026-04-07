# Linking and Reference Guidelines

## Overview

Linking is part of the product quality of the docs system. A strong document should help the reader move sideways to related topics and downward to implementation evidence.

## Relative Links

Within `docs/`, prefer relative Markdown links that work in the repository view.

Examples:

- `../api/endpoints.md`
- `request-flow.md`
- `../verification/benchmarks/non-streaming.md`

Keep links short and readable.

## Related Documentation Section

End technical docs with a `## Related Documentation` section that links to the next most useful documents.

Use that section to connect:

- API docs to auth and streaming docs
- architecture docs to neighboring design docs
- deployment docs to configuration and production docs
- verification docs to the architecture they validate

## Code References Section

End technical docs with a `## Code References` section listing the exact files that support the claims.

Prefer:

- file paths that exist today
- the smallest set of files that proves the behavior

Avoid:

- broad directories when exact files are known

## Status Indicators

In `docs/README.md`, use these markers consistently:

- `✅ Complete`
- `🚧 Draft`
- `⚠️ Outdated`

Update status markers when a document becomes accurate, partial, or stale.

## Cross-Document Consistency

When a new document introduces a concept that already has a home elsewhere:

- add the new link
- consider whether the existing doc should link back
- keep naming consistent across both pages
