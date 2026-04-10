# Governance Model

## Overview

The LLM Access Gateway implements a multi-tenant governance system that provides authentication, authorization, quota enforcement, and usage tracking. The governance model ensures that each tenant operates within their allocated resource limits while maintaining complete isolation from other tenants.

The governance system consists of four key components:

1. **Tenant Model**: Organizational units with resource quotas
2. **API Key Authentication**: Secure credential-based access control
3. **Quota Enforcement**: Rate limiting and budget controls
4. **Usage Tracking**: Comprehensive request and token consumption logging

## Tenant Model

### Tenant Structure

Each tenant represents an independent organizational unit with its own resource quotas and API keys. Tenants are defined in the `tenants` table:

```sql
CREATE TABLE tenants (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(255) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    rpm_limit INT NOT NULL DEFAULT 60,
    tpm_limit INT NOT NULL DEFAULT 4000,
    token_budget INT NOT NULL DEFAULT 1000000,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_tenants_name (name)
);
```

### Tenant Quotas

Each tenant has three independent quota controls:

**RPM Limit (Requests Per Minute)**
- Controls the maximum number of requests a tenant can make per minute
- Default: 60 requests/minute
- Set to 0 to disable RPM limiting
- Enforced using sliding window counters

**TPM Limit (Tokens Per Minute)**
- Controls the maximum number of tokens (prompt + completion) a tenant can consume per minute
- Default: 4000 tokens/minute
- Set to 0 to disable TPM limiting
- Includes both prompt and completion tokens

**Token Budget (Total Tokens)**
- Controls the total number of tokens a tenant can consume across all time
- Default: 1,000,000 tokens
- Set to 0 to disable budget limiting
- Checked before each request is admitted

### Tenant Isolation

Tenants are completely isolated from each other:
- Each tenant has separate API keys
- Usage records are partitioned by tenant_id
- Quota enforcement is per-tenant
- No cross-tenant data access is possible

## API Key Authentication

### API Key Structure

API keys are the primary authentication mechanism. Each key belongs to exactly one tenant:

```sql
CREATE TABLE api_keys (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    key_hash CHAR(64) NOT NULL,
    key_prefix VARCHAR(32) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_api_keys_key_hash (key_hash),
    KEY idx_api_keys_tenant_id (tenant_id),
    CONSTRAINT fk_api_keys_tenant_id
        FOREIGN KEY (tenant_id) REFERENCES tenants (id)
);
```

### Security Model

**Hashed Storage**
- Raw API keys are NEVER stored in the database
- Keys are hashed using SHA-256 before storage
- Only the hash is stored in the `key_hash` column
- Authentication compares hashes, not raw keys

```go
func hashAPIKey(rawKey string) string {
    sum := sha256.Sum256([]byte(rawKey))
    return hex.EncodeToString(sum[:])
}
```

**Key Prefix**
- A short prefix (e.g., first 8 characters) is stored for identification
- Allows operators to identify which key was used without exposing the full key
- Useful for debugging and audit logs

**Enable/Disable Control**
- Both API keys and tenants have an `enabled` flag
- A request is rejected if either the API key OR the tenant is disabled
- Allows temporary suspension without deleting credentials

### Authentication Flow

1. Client sends request with `Authorization: Bearer <api-key>` header
2. Gateway extracts the bearer token
3. Gateway hashes the token using SHA-256
4. Gateway looks up the hash in the `api_keys` table
5. Gateway joins with `tenants` table to get tenant information
6. Gateway checks both `api_keys.enabled` and `tenants.enabled`
7. If valid, gateway creates a `Principal` with tenant context
8. Principal is attached to request context for downstream use

```go
type Principal struct {
    Tenant       Tenant
    APIKeyID     uint64
    APIKeyPrefix string
}
```

### Authentication Errors

The gateway returns specific error codes for authentication failures:

- **401 Unauthorized**: Missing Authorization header
- **401 Unauthorized**: Invalid API key format (not "Bearer <token>")
- **401 Unauthorized**: API key not found (hash doesn't match any record)
- **403 Forbidden**: API key or tenant is disabled

## Quota Enforcement

### Enforcement Architecture

The gateway uses a two-tier quota enforcement system:

1. **Redis-based limiter** (primary): Fast, distributed rate limiting using Redis
2. **MySQL-based limiter** (fallback): Database-backed limiting when Redis is unavailable

### RPM and TPM Enforcement

**Pre-Request Admission Check**

Before processing any request, the governance service performs an admission check:

```go
func (s Service) BeginRequest(ctx context.Context, metadata RequestMetadata) (RequestTracker, error) {
    principal, ok := auth.PrincipalFromContext(ctx)
    if !ok {
        return nil, ErrPrincipalNotFound
    }

    estimatedPromptTokens := estimatePromptTokens(metadata.Messages)

    // Check token budget
    if principal.Tenant.TokenBudget > 0 {
        totalTokensUsed, err := s.store.SumTotalTokens(ctx, principal.Tenant.ID)
        if err != nil {
            return nil, err
        }
        if totalTokensUsed+estimatedPromptTokens > principal.Tenant.TokenBudget {
            return nil, ErrBudgetExceeded
        }
    }

    // Check RPM and TPM limits
    if s.limiter != nil {
        if err := s.limiter.Admit(ctx, principal, estimatedPromptTokens, s.now()); err != nil {
            return nil, err
        }
    }

    // ... create usage record and return tracker
}
```

**Redis-Based Limiting**

The Redis limiter uses Lua scripts for atomic operations:

```lua
local rpm_current = tonumber(redis.call('GET', KEYS[1]) or '0')
local tpm_current = tonumber(redis.call('GET', KEYS[2]) or '0')
local rpm_limit = tonumber(ARGV[2])
local prompt_tokens = tonumber(ARGV[3])
local tpm_limit = tonumber(ARGV[4])

if rpm_limit > 0 and (rpm_current + 1) > rpm_limit then
  return {err='RPM_EXCEEDED'}
end

if tpm_limit > 0 and (tpm_current + prompt_tokens) > tpm_limit then
  return {err='TPM_EXCEEDED'}
end

redis.call('SET', KEYS[1], rpm_current + 1, 'EX', ARGV[1])
redis.call('SET', KEYS[2], tpm_current + prompt_tokens, 'EX', ARGV[1])
return {rpm_current + 1, tpm_current + prompt_tokens}
```

**Counter Keys**

Redis keys are structured to support per-minute sliding windows:

```
lag:rpm:<tenant_id>:<YYYYMMDDHHMM>
lag:tpm:<tenant_id>:<YYYYMMDDHHMM>
```

Example: `lag:rpm:1:202401151430` tracks RPM for tenant 1 during minute 14:30

**TTL Management**

Counters expire after 2 minutes to prevent unbounded memory growth while allowing for clock skew.

**Completion Token Recording**

For streaming requests, completion tokens are not known until the stream completes. The limiter records completion tokens after the fact:

```go
func (l RedisLimiter) RecordCompletionTokens(ctx context.Context, principal auth.Principal, completionTokens int, now time.Time) error {
    if completionTokens <= 0 || principal.Tenant.TPMLimit <= 0 {
        return nil
    }

    if _, err := l.client.IncrBy(ctx, minuteCounterKey("tpm", principal.Tenant.ID, now), int64(completionTokens), l.ttl); err != nil {
        if l.fallback != nil {
            return l.fallback.RecordCompletionTokens(ctx, principal, completionTokens, now)
        }
        return err
    }

    return nil
}
```

### Token Budget Enforcement

Token budget is enforced differently from RPM/TPM:

1. Budget is checked against provider-attempt usage, not only logical request records
2. Check happens before the request is admitted and again before each upstream retry or fallback attempt
3. Uses estimated prompt tokens for admission and attempt reservation
4. Actual tokens are recorded after completion when the provider reports them or when stream usage is finalized

**Budget Check Query**

```sql
SELECT COALESCE(SUM(total_tokens), 0)
FROM request_attempt_usages
WHERE tenant_id = ?
```

### Quota Rejection Behavior

When a quota is exceeded, the gateway returns:

- **429 Too Many Requests**: RPM limit exceeded
- **429 Too Many Requests**: TPM limit exceeded  
- **403 Forbidden**: Token budget exceeded

The response includes an error message indicating which limit was hit.

## Usage Tracking

### Request Usage Records

Every logical client request creates a usage record in the `request_usages` table:

```sql
CREATE TABLE request_usages (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    request_id VARCHAR(255) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    api_key_id BIGINT UNSIGNED NOT NULL,
    model VARCHAR(255) NOT NULL DEFAULT '',
    stream BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(16) NOT NULL,
    prompt_tokens INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    total_tokens INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_request_usages_tenant_created_at (tenant_id, created_at),
    KEY idx_request_usages_request_id (request_id),
    CONSTRAINT fk_request_usages_tenant_id
        FOREIGN KEY (tenant_id) REFERENCES tenants (id),
    CONSTRAINT fk_request_usages_api_key_id
        FOREIGN KEY (api_key_id) REFERENCES api_keys (id)
);
```

### Provider Attempt Usage Records

Every upstream provider attempt creates a record in `request_attempt_usages`. This includes adapter retries and router fallback attempts, so long-term budget enforcement and usage summary totals reflect actual provider work rather than only the final successful response.

### Usage Record Lifecycle

**1. Request Start**

When a request begins, a usage record is created with status "started":

```go
recordID, err := s.store.InsertUsageRecord(ctx, UsageRecord{
    RequestID:    metadata.RequestID,
    TenantID:     principal.Tenant.ID,
    APIKeyID:     principal.APIKeyID,
    Model:        metadata.Model,
    Stream:       metadata.Stream,
    Status:       "started",
    PromptTokens: estimatedPromptTokens,
    TotalTokens:  estimatedPromptTokens,
    CreatedAt:    s.now(),
})
```

**2. Request Completion**

When the request completes successfully, the record is updated with actual token counts:

```go
return t.store.UpdateUsageRecord(ctx, UsageUpdate{
    ID:               t.recordID,
    TenantID:         t.principal.Tenant.ID,
    Model:            resp.Model,
    Status:           "succeeded",
    PromptTokens:     promptTokens,
    CompletionTokens: completionTokens,
    TotalTokens:      totalTokens,
})
```

**3. Request Failure**

If the request fails, the record is updated with status "failed":

```go
return t.store.UpdateUsageRecord(ctx, UsageUpdate{
    ID:       t.recordID,
    TenantID: t.principal.Tenant.ID,
    Model:    t.model,
    Status:   "failed",
})
```

### Token Estimation

For admission checks and initial records, the gateway estimates token counts:

**Prompt Token Estimation**

```go
func estimatePromptTokens(messages []chat.Message) int {
    var builder strings.Builder
    for _, message := range messages {
        builder.WriteString(message.Role)
        builder.WriteString(" ")
        builder.WriteString(message.Content)
        builder.WriteString(" ")
    }
    return estimateTextTokens(builder.String())
}

func estimateTextTokens(text string) int {
    text = strings.TrimSpace(text)
    if text == "" {
        return 0
    }
    fields := strings.Fields(text)
    if len(fields) > 0 {
        return len(fields)
    }
    return len([]rune(text))
}
```

This is a simple word-count estimation. For production use, consider using a proper tokenizer like `tiktoken`.

**Completion Token Estimation**

For streaming requests, completion tokens are estimated from the accumulated response text after the stream completes.

### Usage Queries

The governance store provides queries for usage analysis:

**Sum Total Tokens (All Time)**
```go
func (s GovernanceStore) SumTotalTokens(ctx context.Context, tenantID uint64) (int, error)
```

**Sum Total Tokens Since Timestamp**
```go
func (s GovernanceStore) SumTotalTokensSince(ctx context.Context, tenantID uint64, since time.Time) (int, error)
```

**Count Requests Since Timestamp**
```go
func (s GovernanceStore) CountRequestsSince(ctx context.Context, tenantID uint64, since time.Time) (int, error)
```

**List Recent Usage Records**
```go
func (s GovernanceStore) ListRecentUsageRecords(ctx context.Context, tenantID uint64, limit int) ([]usageservice.RecentUsageRecord, error)
```

## Security Considerations

### API Key Security

**Never Store Raw Keys**
- Raw API keys are hashed with SHA-256 before storage
- The database only contains hashes, not recoverable keys
- If the database is compromised, attackers cannot extract working keys

**Key Rotation**
- Tenants can have multiple API keys
- Old keys can be disabled without deleting usage history
- New keys can be issued without affecting existing keys

**Audit Trail**
- Every request records which API key was used (by ID)
- Key prefix is stored for human-readable identification
- Usage records provide complete audit trail

### Tenant Isolation

**Database-Level Isolation**
- All queries filter by `tenant_id`
- Foreign key constraints prevent orphaned records
- No cross-tenant queries are possible

**Context-Based Authorization**
- Principal is extracted from request context
- All operations use the principal's tenant ID
- No way to access another tenant's data

**Quota Isolation**
- Each tenant's quotas are independent
- One tenant hitting limits doesn't affect others
- Redis keys are namespaced by tenant ID

### Request ID Correlation

Every request has a unique `request_id` that:
- Links usage records to specific requests
- Appears in logs for debugging
- Enables end-to-end request tracing
- Helps correlate metrics, logs, and traces

### Denial of Service Protection

**Rate Limiting**
- RPM limits prevent request flooding
- TPM limits prevent token exhaustion attacks
- Budget limits prevent long-term abuse

**Fail-Safe Defaults**
- New tenants get reasonable default limits
- Limits can be adjusted per tenant
- Disabled tenants are immediately blocked

**Graceful Degradation**
- If Redis fails, MySQL limiter provides fallback
- If MySQL fails, requests are rejected (fail closed)
- No unlimited access is ever granted

## Configuration

### Tenant Configuration

Tenants are configured in the database. Example:

```sql
INSERT INTO tenants (name, enabled, rpm_limit, tpm_limit, token_budget)
VALUES ('acme-corp', TRUE, 100, 10000, 5000000);
```

### API Key Generation

API keys should be generated with sufficient entropy:

```bash
# Generate a secure random key
openssl rand -base64 32
```

The key is then hashed and stored:

```sql
INSERT INTO api_keys (tenant_id, key_hash, key_prefix, enabled)
VALUES (
    1,
    SHA2('your-generated-key', 256),
    'your-gen',
    TRUE
);
```

### Limiter Configuration

The governance service is configured with a limiter implementation:

```go
// Redis-based limiter with MySQL fallback
redisLimiter := governance.NewRedisLimiter(redisClient, mysqlLimiter)
governanceService := governance.NewService(governanceStore, redisLimiter)
```

## Monitoring and Observability

### Metrics

The governance system exposes metrics for monitoring:

- `lag_requests_total{tenant, status}`: Total requests per tenant
- `lag_quota_rejections_total{tenant, reason}`: Quota rejections by reason
- `lag_tokens_consumed_total{tenant}`: Total tokens consumed per tenant

### Logs

Governance events are logged with structured fields:

```json
{
  "level": "info",
  "msg": "request admitted",
  "request_id": "req-123",
  "tenant_id": 1,
  "api_key_id": 5,
  "estimated_prompt_tokens": 150
}
```

```json
{
  "level": "warn",
  "msg": "quota exceeded",
  "request_id": "req-456",
  "tenant_id": 2,
  "reason": "RPM_EXCEEDED",
  "current": 61,
  "limit": 60
}
```

### Usage Analysis

Query the `request_usages` table for usage analysis:

```sql
-- Total tokens consumed by tenant
SELECT tenant_id, SUM(total_tokens) as total
FROM request_usages
GROUP BY tenant_id;

-- Request success rate by tenant
SELECT tenant_id,
       COUNT(*) as total_requests,
       SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END) as successful,
       ROUND(100.0 * SUM(CASE WHEN status = 'succeeded' THEN 1 ELSE 0 END) / COUNT(*), 2) as success_rate
FROM request_usages
GROUP BY tenant_id;

-- Token consumption over time
SELECT DATE(created_at) as date,
       tenant_id,
       SUM(total_tokens) as daily_tokens
FROM request_usages
GROUP BY DATE(created_at), tenant_id
ORDER BY date DESC, tenant_id;
```

## Related Documentation

- [Authentication API](../api/authentication.md): API key authentication flow
- [Request Flow](request-flow.md): How governance fits into request processing
- [Observability](observability.md): Metrics, tracing, and logging
- [Multi-Tenant Governance Blog](../blog/006-multi-tenant-governance.md): Deep dive into governance implementation
