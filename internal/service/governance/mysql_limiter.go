package governance

import (
	"context"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/auth"
)

type mysqlLimiterStore interface {
	CountRequestsSince(ctx context.Context, tenantID uint64, since time.Time) (int, error)
	SumTotalTokensSince(ctx context.Context, tenantID uint64, since time.Time) (int, error)
}

type MySQLLimiter struct {
	store mysqlLimiterStore
}

func NewMySQLLimiter(store mysqlLimiterStore) MySQLLimiter {
	return MySQLLimiter{store: store}
}

func (l MySQLLimiter) Admit(ctx context.Context, principal auth.Principal, promptTokens int, now time.Time) error {
	if principal.Tenant.RPMLimit > 0 {
		count, err := l.store.CountRequestsSince(ctx, principal.Tenant.ID, now.Add(-time.Minute))
		if err != nil {
			return err
		}
		if count >= principal.Tenant.RPMLimit {
			return ErrRateLimitExceeded
		}
	}

	if principal.Tenant.TPMLimit > 0 {
		tokensUsedThisMinute, err := l.store.SumTotalTokensSince(ctx, principal.Tenant.ID, now.Add(-time.Minute))
		if err != nil {
			return err
		}
		if tokensUsedThisMinute+promptTokens > principal.Tenant.TPMLimit {
			return ErrTokenLimitExceeded
		}
	}

	return nil
}

func (l MySQLLimiter) RecordCompletionTokens(context.Context, auth.Principal, int, time.Time) error {
	return nil
}
