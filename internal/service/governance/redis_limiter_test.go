package governance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/auth"
)

func TestRedisLimiterMapsRPMExceeded(t *testing.T) {
	limiter := NewRedisLimiter(&stubRedisClient{evalErr: errors.New("ERR RPM_EXCEEDED")}, nil)

	err := limiter.Admit(context.Background(), auth.Principal{
		Tenant: auth.Tenant{ID: 1, RPMLimit: 1},
	}, 1, time.Unix(123, 0))
	if !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatalf("expected ErrRateLimitExceeded, got %v", err)
	}
}

func TestRedisLimiterReturnsUnavailableOnRedisFailure(t *testing.T) {
	fallback := &stubLimiter{admitErr: ErrTokenLimitExceeded}
	limiter := NewRedisLimiter(&stubRedisClient{evalErr: errors.New("dial tcp redis: connection refused")}, fallback)

	err := limiter.Admit(context.Background(), auth.Principal{
		Tenant: auth.Tenant{ID: 1, TPMLimit: 10},
	}, 2, time.Unix(123, 0))
	if !errors.Is(err, ErrLimiterUnavailable) {
		t.Fatalf("expected ErrLimiterUnavailable, got %v", err)
	}
	if fallback.admitCalls != 0 {
		t.Fatalf("expected fallback admit not to run, got %d calls", fallback.admitCalls)
	}
}

func TestRedisLimiterRecordsCompletionTokens(t *testing.T) {
	client := stubRedisClient{}
	limiter := NewRedisLimiter(&client, nil)

	err := limiter.RecordCompletionTokens(context.Background(), auth.Principal{
		Tenant: auth.Tenant{ID: 1, TPMLimit: 10},
	}, 7, time.Unix(123, 0))
	if err != nil {
		t.Fatalf("record completion tokens: %v", err)
	}

	if client.lastIncrKey == "" || client.lastIncrDelta != 7 {
		t.Fatalf("expected redis incr to be called, got %#v", client)
	}
}

type stubRedisClient struct {
	evalErr       error
	incrErr       error
	lastIncrKey   string
	lastIncrDelta int64
}

func (c stubRedisClient) EvalIntArray(context.Context, string, []string, []string) ([]int64, error) {
	if c.evalErr != nil {
		return nil, c.evalErr
	}
	return []int64{1, 1}, nil
}

func (c *stubRedisClient) IncrBy(_ context.Context, key string, delta int64, _ time.Duration) (int64, error) {
	if c.incrErr != nil {
		return 0, c.incrErr
	}
	c.lastIncrKey = key
	c.lastIncrDelta = delta
	return delta, nil
}
