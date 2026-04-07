# Documentation Maintenance Guidelines

## Overview

Documentation should evolve with the codebase, not trail behind it by weeks. When behavior changes, the matching document should be updated in the same change set whenever possible.

## When to Update Docs

Update documentation when any of these change:

- API routes or response contracts
- auth, governance, or quota behavior
- provider routing, fallback, or health logic
- metrics, logs, or tracing behavior
- deployment manifests or bootstrap flow
- benchmark methodology
- failure drill steps or observed outcomes

## Update Workflow

Use this maintenance loop:

1. identify which docs are affected
2. update the technical doc first
3. update navigation or status indicators next
4. run the matching verification
5. note any remaining blockers explicitly

## Verification Expectations

Match the verification to the change:

- doc-only wording change: link and accuracy review
- API behavior change: curl checks for the touched endpoints
- deployment file change: Compose or manifest validation
- routing or observability change: smoke checks, `/readyz`, `/metrics`, or drills as appropriate
- benchmark or failure-drill update: rerun the command and refresh the evidence

## Review Checklist

Before merging documentation changes, confirm:

- commands are copy-pasteable
- code paths exist
- metrics and log names match the code
- related links resolve
- status markers are still accurate
- no claim is based on planned work instead of current behavior

## Versioning and Freshness

This repo does not use a separate docs versioning system today, so freshness is maintained by:

- updating docs alongside code
- marking incomplete pages as `🚧 Draft`
- marking stale pages as `⚠️ Outdated`
- using final review tasks to check navigation and internal links

## Ownership

The engineer changing behavior owns the first doc update for that behavior. Documentation is not a follow-up nice-to-have; it is part of the definition of done for this repository.
