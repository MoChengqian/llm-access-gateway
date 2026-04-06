package usage

import (
	"context"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/auth"
)

func TestGetTenantUsageBuildsSummaryAndRecentRecords(t *testing.T) {
	now := time.Date(2026, 4, 6, 10, 0, 0, 0, time.UTC)
	store := &stubStore{
		requestsLastMinute: 3,
		tokensLastMinute:   120,
		totalTokens:        860,
		recent: []RecentUsageRecord{
			{
				RequestID:        "req-1",
				APIKeyID:         10,
				Model:            "gpt-4o-mini",
				Stream:           true,
				Status:           "succeeded",
				PromptTokens:     12,
				CompletionTokens: 30,
				TotalTokens:      42,
				CreatedAt:        now.Add(-30 * time.Second),
				UpdatedAt:        now.Add(-20 * time.Second),
			},
		},
	}
	service := service{
		store: store,
		now:   func() time.Time { return now },
	}
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant: auth.Tenant{
			ID:          7,
			Name:        "acme",
			RPMLimit:    60,
			TPMLimit:    4000,
			TokenBudget: 1000,
		},
		APIKeyID: 10,
	})

	resp, err := service.GetTenantUsage(ctx, 5)
	if err != nil {
		t.Fatalf("get tenant usage: %v", err)
	}

	if resp.Object != "usage" {
		t.Fatalf("expected usage object, got %s", resp.Object)
	}
	if resp.Tenant.ID != 7 || resp.Tenant.Name != "acme" {
		t.Fatalf("unexpected tenant %#v", resp.Tenant)
	}
	if resp.Summary.RequestsLastMinute != 3 || resp.Summary.TokensLastMinute != 120 {
		t.Fatalf("unexpected summary %#v", resp.Summary)
	}
	if resp.Summary.TotalTokensUsed != 860 || resp.Summary.RemainingTokenBudget != 140 {
		t.Fatalf("unexpected budget summary %#v", resp.Summary)
	}
	if len(resp.Data) != 1 || resp.Data[0].RequestID != "req-1" {
		t.Fatalf("unexpected recent records %#v", resp.Data)
	}
	if store.lastListLimit != 5 {
		t.Fatalf("expected requested limit 5, got %d", store.lastListLimit)
	}
}

func TestGetTenantUsageUsesDefaultAndMaxLimit(t *testing.T) {
	store := &stubStore{}
	service := service{
		store: store,
		now:   time.Now,
	}
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant: auth.Tenant{ID: 1, Name: "acme"},
	})

	if _, err := service.GetTenantUsage(ctx, 0); err != nil {
		t.Fatalf("default limit should succeed: %v", err)
	}
	if store.lastListLimit != 20 {
		t.Fatalf("expected default limit 20, got %d", store.lastListLimit)
	}

	store.lastListLimit = 0
	if _, err := service.GetTenantUsage(ctx, 999); err != nil {
		t.Fatalf("max limit should be capped, got error %v", err)
	}
	if store.lastListLimit != 100 {
		t.Fatalf("expected capped limit 100, got %d", store.lastListLimit)
	}
}

func TestGetTenantUsageRejectsMissingPrincipalAndInvalidLimit(t *testing.T) {
	service := service{store: &stubStore{}, now: time.Now}

	if _, err := service.GetTenantUsage(context.Background(), 10); err != ErrPrincipalNotFound {
		t.Fatalf("expected ErrPrincipalNotFound, got %v", err)
	}
	if _, err := service.GetTenantUsage(auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant: auth.Tenant{ID: 1, Name: "acme"},
	}), -1); err != ErrInvalidLimit {
		t.Fatalf("expected ErrInvalidLimit, got %v", err)
	}
}

type stubStore struct {
	requestsLastMinute int
	tokensLastMinute   int
	totalTokens        int
	recent             []RecentUsageRecord
	lastListLimit      int
}

func (s *stubStore) CountRequestsSince(context.Context, uint64, time.Time) (int, error) {
	return s.requestsLastMinute, nil
}

func (s *stubStore) SumTotalTokensSince(context.Context, uint64, time.Time) (int, error) {
	return s.tokensLastMinute, nil
}

func (s *stubStore) SumTotalTokens(context.Context, uint64) (int, error) {
	return s.totalTokens, nil
}

func (s *stubStore) ListRecentUsageRecords(_ context.Context, _ uint64, limit int) ([]RecentUsageRecord, error) {
	s.lastListLimit = limit
	return s.recent, nil
}
