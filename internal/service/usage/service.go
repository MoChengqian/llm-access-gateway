package usage

import (
	"context"
	"errors"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/auth"
)

const (
	defaultRecentLimit = 20
	maxRecentLimit     = 100
)

var (
	ErrPrincipalNotFound = errors.New("principal not found")
	ErrInvalidLimit      = errors.New("invalid limit")
)

type Store interface {
	CountRequestsSince(ctx context.Context, tenantID uint64, since time.Time) (int, error)
	SumTotalTokensSince(ctx context.Context, tenantID uint64, since time.Time) (int, error)
	SumTotalTokens(ctx context.Context, tenantID uint64) (int, error)
	SumTotalAttemptTokensSince(ctx context.Context, tenantID uint64, since time.Time) (int, error)
	SumTotalAttemptTokens(ctx context.Context, tenantID uint64) (int, error)
	ListRecentUsageRecords(ctx context.Context, tenantID uint64, limit int) ([]RecentUsageRecord, error)
}

type TenantUsageGetter interface {
	GetTenantUsage(ctx context.Context, limit int) (Response, error)
}

type Service = TenantUsageGetter

type service struct {
	store Store
	now   func() time.Time
}

type Response struct {
	Object  string        `json:"object"`
	Tenant  Tenant        `json:"tenant"`
	Summary Summary       `json:"summary"`
	Data    []UsageRecord `json:"data"`
}

type Tenant struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}

type Summary struct {
	WindowSeconds           int `json:"window_seconds"`
	RequestsLastMinute      int `json:"requests_last_minute"`
	TokensLastMinute        int `json:"tokens_last_minute"`
	TotalTokensUsed         int `json:"total_tokens_used"`
	LogicalTokensLastMinute int `json:"logical_tokens_last_minute"`
	LogicalTotalTokensUsed  int `json:"logical_total_tokens_used"`
	RPMLimit                int `json:"rpm_limit"`
	TPMLimit                int `json:"tpm_limit"`
	TokenBudget             int `json:"token_budget"`
	RemainingTokenBudget    int `json:"remaining_token_budget"`
}

type UsageRecord struct {
	RequestID        string    `json:"request_id"`
	APIKeyID         uint64    `json:"api_key_id"`
	Model            string    `json:"model"`
	Stream           bool      `json:"stream"`
	Status           string    `json:"status"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type RecentUsageRecord struct {
	RequestID        string
	APIKeyID         uint64
	Model            string
	Stream           bool
	Status           string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func NewService(store Store) TenantUsageGetter {
	return service{
		store: store,
		now:   time.Now,
	}
}

func (s service) GetTenantUsage(ctx context.Context, limit int) (Response, error) {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return Response{}, ErrPrincipalNotFound
	}

	normalizedLimit, err := normalizeLimit(limit)
	if err != nil {
		return Response{}, err
	}

	windowStart := s.now().Add(-time.Minute)
	requestsLastMinute, err := s.store.CountRequestsSince(ctx, principal.Tenant.ID, windowStart)
	if err != nil {
		return Response{}, err
	}

	logicalTokensLastMinute, err := s.store.SumTotalTokensSince(ctx, principal.Tenant.ID, windowStart)
	if err != nil {
		return Response{}, err
	}

	tokensLastMinute, err := s.store.SumTotalAttemptTokensSince(ctx, principal.Tenant.ID, windowStart)
	if err != nil {
		return Response{}, err
	}

	logicalTotalTokensUsed, err := s.store.SumTotalTokens(ctx, principal.Tenant.ID)
	if err != nil {
		return Response{}, err
	}

	totalTokensUsed, err := s.store.SumTotalAttemptTokens(ctx, principal.Tenant.ID)
	if err != nil {
		return Response{}, err
	}

	recent, err := s.store.ListRecentUsageRecords(ctx, principal.Tenant.ID, normalizedLimit)
	if err != nil {
		return Response{}, err
	}

	data := make([]UsageRecord, 0, len(recent))
	for _, record := range recent {
		data = append(data, UsageRecord{
			RequestID:        record.RequestID,
			APIKeyID:         record.APIKeyID,
			Model:            record.Model,
			Stream:           record.Stream,
			Status:           record.Status,
			PromptTokens:     record.PromptTokens,
			CompletionTokens: record.CompletionTokens,
			TotalTokens:      record.TotalTokens,
			CreatedAt:        record.CreatedAt,
			UpdatedAt:        record.UpdatedAt,
		})
	}

	remainingBudget := 0
	if principal.Tenant.TokenBudget > 0 {
		remainingBudget = principal.Tenant.TokenBudget - totalTokensUsed
		if remainingBudget < 0 {
			remainingBudget = 0
		}
	}

	return Response{
		Object: "usage",
		Tenant: Tenant{
			ID:   principal.Tenant.ID,
			Name: principal.Tenant.Name,
		},
		Summary: Summary{
			WindowSeconds:           60,
			RequestsLastMinute:      requestsLastMinute,
			TokensLastMinute:        tokensLastMinute,
			TotalTokensUsed:         totalTokensUsed,
			LogicalTokensLastMinute: logicalTokensLastMinute,
			LogicalTotalTokensUsed:  logicalTotalTokensUsed,
			RPMLimit:                principal.Tenant.RPMLimit,
			TPMLimit:                principal.Tenant.TPMLimit,
			TokenBudget:             principal.Tenant.TokenBudget,
			RemainingTokenBudget:    remainingBudget,
		},
		Data: data,
	}, nil
}

func normalizeLimit(limit int) (int, error) {
	if limit == 0 {
		return defaultRecentLimit, nil
	}
	if limit < 0 {
		return 0, ErrInvalidLimit
	}
	if limit > maxRecentLimit {
		return maxRecentLimit, nil
	}
	return limit, nil
}
