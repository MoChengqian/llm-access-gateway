# Writing Style Guidelines

## Overview

The documentation in this repository is meant to be:

- evidence-based
- easy to scan
- grounded in the current codebase
- honest about system boundaries

The goal is not to sound generic or exhaustive for its own sake. The goal is to help an engineer, interviewer, or operator understand what the gateway actually does today.

## Document Structure

Use a predictable structure for technical documents:

1. title
2. overview
3. main technical sections
4. related documentation
5. code references

If a document is operational, add verification and troubleshooting sections. If it is analytical, add results and analysis sections.

## Tone

Use direct technical prose:

- short paragraphs
- specific verbs
- concrete nouns
- minimal filler

Prefer:

- “The router skips unhealthy backends during cooldown.”

Avoid:

- “The system leverages an advanced mechanism to intelligently handle backend conditions.”

## Scope Discipline

Only document behavior that exists in code, tests, or measured output.

When the repository does not support something yet:

- say so plainly
- explain the current boundary
- do not imply planned work is already implemented

## Audience Awareness

Keep the reader in mind:

- engineers need implementation detail and runnable commands
- interviewers need rationale and evidence
- operators need health, readiness, and debugging paths

Choose the explanation depth that fits the document’s audience.

## Required Closing Sections

Every technical document should end with:

- `## Related Documentation`
- `## Code References`

These sections keep navigation and evidence tight across the system.
