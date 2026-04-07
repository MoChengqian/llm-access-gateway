# Failure Drill Template

## Overview

State the failure scenario in one paragraph:

- what is being broken on purpose
- why this scenario matters
- what success looks like

## Scenario

Describe the exact trigger:

- provider timeout
- provider 5xx
- quota rejection
- stream interruption before or after first chunk

## Expected Behavior

Document the expected response before running the drill:

- HTTP status
- fallback behavior
- readiness behavior
- log, metric, or trace signals

## Setup

Document the environment assumptions and any toggles required.

```bash
export APP_GATEWAY_PRIMARY_MOCK_FAIL_CREATE='true'
```

## Reproduction Steps

Provide the actual commands in order.

```bash
./scripts/provider-fallback-drill.sh create-fail
```

```bash
curl -i http://127.0.0.1:8080/debug/providers
```

## Observed Behavior

Split evidence by source:

### HTTP Output

```text
HTTP/1.1 200 OK
```

### Logs

```json
{"msg":"provider event", ...}
```

### Metrics

```text
lag_provider_events_total{...}
```

### Trace or Correlation Fields

Document `X-Request-Id`, `X-Trace-Id`, or span logs when relevant.

## Outcome

Answer clearly:

- did the system behave as designed
- what boundary was confirmed
- what gap or risk remains

## Cleanup

Document how to return the system to a clean state.

## Related Documentation

- [Routing and Resilience](../../architecture/routing-resilience.md)

Replace this list with the correct drill neighbors.

## Code References

- [`scripts/provider-fallback-drill.sh`](../../../scripts/provider-fallback-drill.sh)
- [`internal/provider/router/chat.go`](../../../internal/provider/router/chat.go)

Replace or extend with the exact files involved in the scenario.
