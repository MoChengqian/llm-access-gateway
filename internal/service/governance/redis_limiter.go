package governance

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/auth"
)

const admitScript = `
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
`

type RedisLimiter struct {
	client   redisCounterClient
	fallback Limiter
	ttl      time.Duration
}

type redisCounterClient interface {
	EvalIntArray(ctx context.Context, script string, keys []string, args []string) ([]int64, error)
	IncrBy(ctx context.Context, key string, delta int64, ttl time.Duration) (int64, error)
}

func NewRedisLimiter(client redisCounterClient, fallback Limiter) RedisLimiter {
	return RedisLimiter{
		client:   client,
		fallback: fallback,
		ttl:      2 * time.Minute,
	}
}

func (l RedisLimiter) Admit(ctx context.Context, principal auth.Principal, promptTokens int, now time.Time) error {
	if principal.Tenant.RPMLimit <= 0 && principal.Tenant.TPMLimit <= 0 {
		if l.fallback != nil {
			return l.fallback.Admit(ctx, principal, promptTokens, now)
		}
		return nil
	}

	keys := []string{
		minuteCounterKey("rpm", principal.Tenant.ID, now),
		minuteCounterKey("tpm", principal.Tenant.ID, now),
	}
	args := []string{
		fmt.Sprintf("%d", int(l.ttl.Seconds())),
		fmt.Sprintf("%d", principal.Tenant.RPMLimit),
		fmt.Sprintf("%d", promptTokens),
		fmt.Sprintf("%d", principal.Tenant.TPMLimit),
	}

	if _, err := l.client.EvalIntArray(ctx, admitScript, keys, args); err != nil {
		switch {
		case strings.Contains(err.Error(), "RPM_EXCEEDED"):
			return ErrRateLimitExceeded
		case strings.Contains(err.Error(), "TPM_EXCEEDED"):
			return ErrTokenLimitExceeded
		default:
			if l.fallback != nil {
				return l.fallback.Admit(ctx, principal, promptTokens, now)
			}
			return err
		}
	}

	return nil
}

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

func minuteCounterKey(kind string, tenantID uint64, now time.Time) string {
	return fmt.Sprintf("lag:%s:%d:%s", kind, tenantID, now.UTC().Format("200601021504"))
}
