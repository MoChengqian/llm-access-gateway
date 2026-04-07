# Authentication

This document describes the authentication mechanism used by the LLM Access Gateway to secure API endpoints and identify tenants.

## Overview

The gateway uses API key-based authentication with Bearer token authorization. Each API key is associated with a tenant, and all authenticated requests are subject to the tenant's governance policies (rate limits, token quotas, and budgets).

## Authentication Flow

1. **Client includes API key**: The client sends an API key in the `Authorization` header using the Bearer scheme.
2. **Gateway validates key**: The gateway hashes the provided key and looks it up in the database.
3. **Gateway checks status**: The gateway verifies that both the API key and its associated tenant are enabled.
4. **Gateway resolves tenant**: If authentication succeeds, the gateway resolves the tenant and applies governance policies.
5. **Request proceeds**: The authenticated request proceeds to governance checks and provider routing.

## API Key Format

API keys are arbitrary strings provided by the client. The gateway stores only the SHA-256 hash of each key, never the raw key itself.

**Example API Key**:
```
lag-local-dev-key
```

For local development, the default API key created by `go run ./cmd/devinit` is `lag-local-dev-key`.

## Authorization Header

All protected endpoints require an `Authorization` header with the Bearer scheme:

```
Authorization: Bearer <api-key>
```

**Example**:
```
Authorization: Bearer lag-local-dev-key
```

## Protected Endpoints

The following endpoints require authentication:

- `POST /v1/chat/completions` - Create chat completions
- `GET /v1/models` - List available models
- `GET /v1/usage` - Get tenant usage information

## Public Endpoints

The following endpoints do not require authentication:

- `GET /healthz` - Liveness check
- `GET /readyz` - Readiness check
- `GET /debug/providers` - Provider health inspection
- `GET /metrics` - Prometheus metrics

## Authentication Errors

### Missing API Key

**Condition**: The `Authorization` header is missing or empty.

**Status Code**: `401 Unauthorized`

**Response**:
```json
{
  "error": "missing api key"
}
```

**Example**:
```bash
curl -i http://127.0.0.1:8080/v1/models
```

**Response**:
```
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{
  "error": "missing api key"
}
```

### Invalid API Key

**Condition**: The provided API key does not exist in the database, or the `Authorization` header format is incorrect.

**Status Code**: `401 Unauthorized`

**Response**:
```json
{
  "error": "invalid api key"
}
```

**Example - Wrong Key**:
```bash
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer wrong-key-12345'
```

**Response**:
```
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{
  "error": "invalid api key"
}
```

**Example - Malformed Header**:
```bash
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: lag-local-dev-key'
```

**Response**:
```
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{
  "error": "invalid api key"
}
```

### Disabled API Key

**Condition**: The API key exists but has been disabled, or the tenant associated with the key has been disabled.

**Status Code**: `401 Unauthorized`

**Response**:
```json
{
  "error": "disabled api key"
}
```

**Example**:
```bash
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer disabled-key'
```

**Response**:
```
HTTP/1.1 401 Unauthorized
Content-Type: application/json

{
  "error": "disabled api key"
}
```

## Security Considerations

### Key Storage

- **Raw keys are never stored**: The gateway stores only SHA-256 hashes of API keys in the database.
- **Hash comparison**: Authentication compares the hash of the provided key with stored hashes.
- **Key prefix**: A short prefix (first few characters) is stored for display purposes in logs and admin interfaces, but never the full key.

### Tenant Isolation

- **Tenant resolution**: Each API key is associated with exactly one tenant.
- **Governance enforcement**: All requests are subject to the tenant's RPM, TPM, and token budget limits.
- **Usage tracking**: Token usage is tracked per tenant for billing and quota enforcement.

### Key Management

- **Key generation**: API keys should be generated with sufficient entropy (e.g., cryptographically random strings).
- **Key rotation**: Tenants can have multiple active API keys to support key rotation without downtime.
- **Key revocation**: API keys can be disabled without deleting them, preserving usage history.

## Implementation Details

### Authentication Service

The authentication logic is implemented in `internal/auth/service.go`:

```go
func (s Service) AuthenticateRequest(ctx context.Context, authorization string) (Principal, error) {
    rawKey, err := bearerToken(authorization)
    if err != nil {
        return Principal{}, err
    }

    record, err := s.store.LookupAPIKey(ctx, hashAPIKey(rawKey))
    if err != nil {
        if errors.Is(err, ErrAPIKeyNotFound) || errors.Is(err, sql.ErrNoRows) {
            return Principal{}, ErrInvalidAPIKey
        }
        return Principal{}, err
    }

    if !record.APIKeyEnabled || !record.TenantEnabled {
        return Principal{}, ErrDisabledAPIKey
    }

    return Principal{
        Tenant:       record.Tenant,
        APIKeyID:     record.APIKeyID,
        APIKeyPrefix: record.APIKeyPrefix,
    }, nil
}
```

### Key Hashing

API keys are hashed using SHA-256:

```go
func hashAPIKey(rawKey string) string {
    sum := sha256.Sum256([]byte(rawKey))
    return hex.EncodeToString(sum[:])
}
```

### Bearer Token Parsing

The `Authorization` header is parsed to extract the Bearer token:

```go
func bearerToken(authorization string) (string, error) {
    if strings.TrimSpace(authorization) == "" {
        return "", ErrMissingAPIKey
    }

    parts := strings.SplitN(strings.TrimSpace(authorization), " ", 2)
    if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
        return "", ErrInvalidAPIKey
    }

    token := strings.TrimSpace(parts[1])
    if token == "" {
        return "", ErrInvalidAPIKey
    }

    return token, nil
}
```

## Testing Authentication

### Valid Authentication

```bash
# Using the local development key
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer lag-local-dev-key'
```

**Expected**: `200 OK` with model list

### Missing Authorization Header

```bash
curl -i http://127.0.0.1:8080/v1/models
```

**Expected**: `401 Unauthorized` with `{"error":"missing api key"}`

### Invalid Key Format

```bash
# Missing "Bearer" prefix
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: lag-local-dev-key'
```

**Expected**: `401 Unauthorized` with `{"error":"invalid api key"}`

### Wrong API Key

```bash
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer invalid-key-xyz'
```

**Expected**: `401 Unauthorized` with `{"error":"invalid api key"}`

### Empty Bearer Token

```bash
curl -i http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer '
```

**Expected**: `401 Unauthorized` with `{"error":"invalid api key"}`

## Related Documentation

- [API Endpoints](endpoints.md) - Complete API endpoint documentation
- [Architecture: Governance](../architecture/governance.md) - Multi-tenant governance model
- [GET /v1/usage](endpoints.md#get-v1usage) - View tenant usage and quotas

## Database Schema

API keys and tenants are stored in the following tables:

**tenants table**:
```sql
CREATE TABLE tenants (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    rpm_limit INT NOT NULL DEFAULT 60,
    tpm_limit INT NOT NULL DEFAULT 4000,
    token_budget INT NOT NULL DEFAULT 1000000,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);
```

**api_keys table**:
```sql
CREATE TABLE api_keys (
    id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id BIGINT UNSIGNED NOT NULL,
    key_hash VARCHAR(64) NOT NULL UNIQUE,
    key_prefix VARCHAR(16) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (tenant_id) REFERENCES tenants(id)
);
```

The `key_hash` column stores the SHA-256 hash of the API key, and `key_prefix` stores the first few characters for display purposes.
