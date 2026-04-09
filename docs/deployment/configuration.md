# Configuration Reference

## Overview

The gateway loads configuration from two sources:

1. [`configs/config.yaml`](../../configs/config.yaml)
2. `APP_*` environment variables loaded by Viper in [`internal/config/config.go`](../../internal/config/config.go)

Environment variables override values from `config.yaml`. The loader uses:

- env prefix: `APP`
- key replacer: `.` -> `_`

So `server.address` becomes `APP_SERVER_ADDRESS`, and `provider.primary.max_retries` becomes `APP_PROVIDER_PRIMARY_MAX_RETRIES`.

## Configuration Sources

### YAML file

`configs/config.yaml` contains the baseline development defaults for:

- server timeouts and request body size
- log level
- MySQL and Redis connection settings
- gateway routing/health defaults
- provider definitions for `primary` and `secondary`
- optional multi-backend routing definitions under `provider.backends`

### Environment variables

`Load()` calls `v.AutomaticEnv()`, so any supported field can be overridden by environment variables. This is how the Docker Compose and Kubernetes deployments inject environment-specific settings without editing the YAML file.

## Server Configuration

### YAML keys

```yaml
server:
  address: ":8080"
  read_header_timeout_seconds: 5
  read_timeout_seconds: 15
  write_timeout_seconds: 60
  idle_timeout_seconds: 60
  max_request_body_bytes: 1048576
```

### Environment variables

- `APP_SERVER_ADDRESS`
- `APP_SERVER_READ_HEADER_TIMEOUT_SECONDS`
- `APP_SERVER_READ_TIMEOUT_SECONDS`
- `APP_SERVER_WRITE_TIMEOUT_SECONDS`
- `APP_SERVER_IDLE_TIMEOUT_SECONDS`
- `APP_SERVER_MAX_REQUEST_BODY_BYTES`

These values map directly to the `http.Server` created in [`cmd/gateway/main.go`](../../cmd/gateway/main.go).

## Log Configuration

### YAML

```yaml
log:
  level: info
```

### Environment variable

- `APP_LOG_LEVEL`

The level is applied to the zap production logger at startup.

## MySQL and Redis

### YAML

```yaml
mysql:
  dsn: ""

redis:
  address: ""
  password: ""
  db: 0
```

### Environment variables

- `APP_MYSQL_DSN`
- `APP_REDIS_ADDRESS`
- `APP_REDIS_PASSWORD`
- `APP_REDIS_DB`

Operational notes:

- `APP_MYSQL_DSN` is required for `cmd/gateway`
- if Redis is configured but unavailable, the gateway logs the ping failure and falls back to the MySQL limiter
- if Redis is omitted entirely, MySQL remains the limiter backend

## Gateway Behavior Controls

### YAML

```yaml
gateway:
  default_model: gpt-4o-mini
  provider_failure_threshold: 1
  provider_cooldown_seconds: 30
  provider_probe_interval_seconds: 30
  primary_mock_fail_create: false
  primary_mock_fail_stream: false
```

### Environment variables

- `APP_GATEWAY_DEFAULT_MODEL`
- `APP_GATEWAY_PROVIDER_FAILURE_THRESHOLD`
- `APP_GATEWAY_PROVIDER_COOLDOWN_SECONDS`
- `APP_GATEWAY_PROVIDER_PROBE_INTERVAL_SECONDS`
- `APP_GATEWAY_PRIMARY_MOCK_FAIL_CREATE`
- `APP_GATEWAY_PRIMARY_MOCK_FAIL_STREAM`

These settings control:

- the fallback model when no model is specified by the request
- how many consecutive failures trigger cooldown
- how long cooldown lasts
- how often the background provider probe loop runs
- whether the primary mock backend fails for create or stream paths

## Provider Configuration

The gateway supports two provider configuration styles:

1. legacy named providers:
   - `provider.primary`
   - `provider.secondary`
2. preferred multi-backend list:
   - `provider.backends[]`

Each provider block uses the same core fields:

```yaml
provider:
  primary:
    type: mock
    name: primary
    priority: 100
    base_url: ""
    api_key: ""
    model: ""
    models: []
    max_tokens: 1024
    timeout_seconds: 15
    max_retries: 1
    retry_backoff_milliseconds: 200
```

Environment variable pattern:

- `APP_PROVIDER_PRIMARY_TYPE`
- `APP_PROVIDER_PRIMARY_NAME`
- `APP_PROVIDER_PRIMARY_PRIORITY`
- `APP_PROVIDER_PRIMARY_BASE_URL`
- `APP_PROVIDER_PRIMARY_API_KEY`
- `APP_PROVIDER_PRIMARY_MODEL`
- `APP_PROVIDER_PRIMARY_MAX_TOKENS`
- `APP_PROVIDER_PRIMARY_TIMEOUT_SECONDS`
- `APP_PROVIDER_PRIMARY_MAX_RETRIES`
- `APP_PROVIDER_PRIMARY_RETRY_BACKOFF_MILLISECONDS`

Equivalent `SECONDARY` variables exist for the secondary backend.

When you need more than two backends or model-aware routing, define `provider.backends` in YAML. The repository currently documents and tests list-style routing through YAML, not through environment-variable expansion for list items:

```yaml
provider:
  backends:
    - name: openai-gpt4o
      type: openai
      priority: 10
      models: ["gpt-4o-mini"]
      base_url: "https://api.openai.com/v1"
      api_key: "${OPENAI_API_KEY}"
      model: "gpt-4o-mini"
      timeout_seconds: 15
      max_retries: 1
      retry_backoff_milliseconds: 200
    - name: anthropic-sonnet
      type: anthropic
      priority: 20
      models: ["claude-3-5-sonnet-latest"]
      base_url: "https://api.anthropic.com/v1"
      api_key: "${ANTHROPIC_API_KEY}"
      model: "claude-3-5-sonnet-latest"
      max_tokens: 1024
      timeout_seconds: 15
      max_retries: 1
      retry_backoff_milliseconds: 200
    - name: generic-fallback
      type: mock
      priority: 100
      models: []
```

Routing semantics:

- lower `priority` values are attempted first
- `models[]` is an exact-match preference list for request models
- backends with empty `models[]` are generic fallbacks
- if `provider.backends` is present, it replaces legacy `primary` and `secondary` assembly

### Supported types

- `mock`
- `openai`
- `anthropic`
- `ollama`

`buildProviderBackend()` defaults empty provider types to `mock`. For `openai`, `base_url` is required and should already include the upstream `/v1` base path. For `anthropic`, `base_url` should point at the Anthropic API root such as `https://api.anthropic.com/v1`; the adapter automatically sends `x-api-key` and `anthropic-version` headers, translates OpenAI-style `system` messages into Anthropic's top-level `system` field, and requires `max_tokens` (default `1024`). For `ollama`, `base_url` should point at the Ollama server root such as `http://127.0.0.1:11434`.

## Example Configurations

### Local default path

```bash
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'
go run ./cmd/devinit
go run ./cmd/gateway
```

### Real OpenAI-compatible primary with mock secondary

```bash
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'
export APP_PROVIDER_PRIMARY_TYPE='openai'
export APP_PROVIDER_PRIMARY_BASE_URL='https://api.openai.com/v1'
export APP_PROVIDER_PRIMARY_API_KEY='sk-...'
export APP_PROVIDER_PRIMARY_MODEL='gpt-4.1-mini'
export APP_PROVIDER_PRIMARY_TIMEOUT_SECONDS='15'
export APP_PROVIDER_PRIMARY_MAX_RETRIES='1'
export APP_PROVIDER_PRIMARY_RETRY_BACKOFF_MILLISECONDS='200'
go run ./cmd/gateway
```

### Real Anthropic primary with mock secondary

```bash
export APP_MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/llm_access_gateway?parseTime=true'
export APP_REDIS_ADDRESS='127.0.0.1:6379'
export APP_PROVIDER_PRIMARY_TYPE='anthropic'
export APP_PROVIDER_PRIMARY_BASE_URL='https://api.anthropic.com/v1'
export APP_PROVIDER_PRIMARY_API_KEY='sk-ant-...'
export APP_PROVIDER_PRIMARY_MODEL='claude-3-5-sonnet-latest'
export APP_PROVIDER_PRIMARY_MAX_TOKENS='1024'
export APP_PROVIDER_PRIMARY_TIMEOUT_SECONDS='15'
export APP_PROVIDER_PRIMARY_MAX_RETRIES='1'
export APP_PROVIDER_PRIMARY_RETRY_BACKOFF_MILLISECONDS='200'
go run ./cmd/gateway
```

## Configuration Caveats

- Secrets such as upstream API keys and MySQL DSNs should come from environment variables or Kubernetes Secrets, not committed YAML.
- The provider router is deterministic failover. It now supports exact model matching plus explicit numeric priority, but it still does not implement weighted balancing.
- Mock failure toggles are useful for drills and local verification but should not be enabled in normal production environments.
- `gateway.primary_mock_fail_*` only affects the legacy `provider.primary` path, not `provider.backends[]`.
- `max_tokens` is currently consumed by the Anthropic adapter. OpenAI-compatible and Ollama adapters ignore it today because the shared gateway request contract does not expose provider-specific generation parameters yet.

## Related Documentation

- [Docker Compose Deployment](docker-compose.md)
- [Kubernetes Deployment](kubernetes.md)
- [Production Considerations](production-considerations.md)
- [Routing and Resilience](../architecture/routing-resilience.md)

## Code References

- [`configs/config.yaml`](../../configs/config.yaml)
- [`internal/config/config.go`](../../internal/config/config.go)
- [`cmd/gateway/main.go`](../../cmd/gateway/main.go)
- [`internal/provider/anthropic/chat.go`](../../internal/provider/anthropic/chat.go)
- [`internal/provider/openai/chat.go`](../../internal/provider/openai/chat.go)
- [`internal/provider/router/chat.go`](../../internal/provider/router/chat.go)
