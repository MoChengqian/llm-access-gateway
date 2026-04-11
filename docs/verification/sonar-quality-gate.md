# SonarCloud Main-Branch Quality Gate

## Purpose

GitHub Actions green does not prove that SonarCloud computed a quality gate for
the `main` branch. This page documents the explicit verification path for the
repository's external SonarCloud gate.

## Verification Command

Run:

```bash
make sonar-quality-gate-check
```

The command calls SonarCloud's public API and prints:

- project key
- inspected scope
- assigned quality gate
- last analysis timestamp
- computed gate status
- dashboard URL

## Status Meanings

- `OK`: SonarCloud computed the gate and it passed.
- `ERROR` or `WARN`: SonarCloud computed the gate and at least one condition did
  not pass.
- `NONE`: SonarCloud analyzed the branch but did not compute the gate.

`NONE` is the important ambiguous case. It means the merge commit may look fine
in GitHub while the external quality-gate contract is still incomplete.

## Repository Configuration

The repository keeps SonarCloud project metadata in:

```text
sonar-project.properties
```

The file intentionally excludes provider adapter implementations from copy-paste
detection. Those adapters share HTTP client, streaming, and test scaffolding,
but their request translation and provider compatibility behavior are reviewed
through unit tests, drills, and runtime CI.

Do not broaden `sonar.cpd.exclusions` to unrelated application, routing,
persistence, or deployment code. If duplication appears outside provider
adapter scaffolding, fix the code instead of hiding it.

## Current Repository-Specific Interpretation

If `main` reports `NONE` while pull requests report `OK`, treat that as a
SonarCloud project-configuration issue, not an immediate code regression.

For this repository, the likely missing piece is the New Code Definition for
`main`.

## Remediation

In SonarCloud, as a project admin:

1. Open the project dashboard.
2. Go to `Administration -> New Code`.
3. Configure a baseline for `main`.

Recommended baseline choices:

- `Previous version`, if releases and tags will be maintained
- `Number of days`, only if the team explicitly wants a rolling window
- `Reference branch`, only if another long-lived branch is intentionally the baseline

After updating the setting, rerun analysis and verify:

```bash
make sonar-quality-gate-check
```

## Non-Goals

- This repository does not store SonarCloud admin credentials.
- This repository does not auto-mutate SonarCloud project settings.
- `stage7-static` does not call SonarCloud, because it must remain offline-capable.
