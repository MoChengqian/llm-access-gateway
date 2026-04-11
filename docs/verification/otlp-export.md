# OTLP Export Verification

## Goal

Prove that the gateway does more than parse OTLP configuration: it can emit a
real OTLP/HTTP trace payload to an external receiver.

## Repo-Native Verification Path

The repository includes two helper assets for this check:

- `cmd/otlpstub`: a small local OTLP/HTTP receiver that records the last trace
  export request
- `scripts/otlp-export-check.sh`: starts the stub, triggers a traced request
  against a running gateway, and waits for the exported payload

## Local Run

Start the gateway with OTLP export enabled:

```bash
export APP_OBSERVABILITY_SERVICE_NAME='llm-access-gateway'
export APP_OBSERVABILITY_OTLP_TRACES_ENDPOINT='http://127.0.0.1:4318/v1/traces'
export APP_OBSERVABILITY_OTLP_EXPORT_TIMEOUT_SECONDS='1'
go run ./cmd/gateway
```

Then run:

```bash
./scripts/otlp-export-check.sh
make otlp-check
```

The check passes when:

- the stub becomes healthy on `127.0.0.1:4318`
- the gateway responds on `/healthz`
- the capture file shows at least one `POST` to `/v1/traces`

Example capture:

```json
{
  "request_count": 1,
  "method": "POST",
  "path": "/v1/traces",
  "content_type": "application/x-protobuf",
  "content_length": 732,
  "updated_at": "2026-04-11T00:00:00Z"
}
```

## Why This Matters

This check keeps the observability story reproducible:

- exporter configuration is verified end to end
- no external SaaS or long-lived collector is required
- regressions in OTLP path handling, content type, or batch export show up in
  a repository-owned script and test flow
