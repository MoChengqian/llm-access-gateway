package governance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/auth"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
)

func TestBeginRequestRejectsWhenPrincipalMissing(t *testing.T) {
	service := NewService(&stubStore{})

	_, err := service.BeginRequest(context.Background(), RequestMetadata{})
	if !errors.Is(err, ErrPrincipalNotFound) {
		t.Fatalf("expected ErrPrincipalNotFound, got %v", err)
	}
}

func TestBeginRequestRejectsWhenRPMLimitExceeded(t *testing.T) {
	service := NewService(&stubStore{count: 2})
	service.now = func() time.Time { return time.Unix(123, 0) }

	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 9, Name: "acme", RPMLimit: 2},
		APIKeyID: 7,
	})

	_, err := service.BeginRequest(ctx, RequestMetadata{RequestID: "req-1"})
	if !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatalf("expected ErrRateLimitExceeded, got %v", err)
	}
}

func TestBeginRequestInsertsStartedUsageRecord(t *testing.T) {
	store := &stubStore{insertID: 11}
	service := NewService(store)
	service.now = func() time.Time { return time.Unix(123, 0) }

	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:       auth.Tenant{ID: 9, Name: "acme", RPMLimit: 5},
		APIKeyID:     7,
		APIKeyPrefix: "lag-abc",
	})

	tracker, err := service.BeginRequest(ctx, RequestMetadata{
		RequestID: "req-1",
		Model:     "gpt-4o-mini",
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("begin request: %v", err)
	}

	if tracker == nil {
		t.Fatal("expected tracker")
	}

	if store.inserted.RequestID != "req-1" {
		t.Fatalf("expected request id req-1, got %#v", store.inserted)
	}

	if store.inserted.TenantID != 9 || store.inserted.APIKeyID != 7 {
		t.Fatalf("expected tenant/api key ids in inserted record, got %#v", store.inserted)
	}

	if store.inserted.Status != "started" || !store.inserted.Stream {
		t.Fatalf("expected started stream record, got %#v", store.inserted)
	}
}

func TestCompleteNonStreamUsesProviderUsageWhenPresent(t *testing.T) {
	store := &stubStore{insertID: 21}
	service := NewService(store)
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 1, Name: "acme", RPMLimit: 10},
		APIKeyID: 2,
	})

	tracker, err := service.BeginRequest(ctx, RequestMetadata{RequestID: "req-1"})
	if err != nil {
		t.Fatalf("begin request: %v", err)
	}

	err = tracker.CompleteNonStream(context.Background(), chat.CompletionRequest{
		Messages: []chat.Message{{Role: "user", Content: "hello world"}},
	}, chat.CompletionResponse{
		Model: "gpt-4o-mini",
		Usage: chat.Usage{
			PromptTokens:     5,
			CompletionTokens: 7,
			TotalTokens:      12,
		},
	})
	if err != nil {
		t.Fatalf("complete non stream: %v", err)
	}

	if store.updated.TotalTokens != 12 || store.updated.CompletionTokens != 7 {
		t.Fatalf("expected provider usage to be kept, got %#v", store.updated)
	}
}

func TestCompleteStreamAggregatesChunkContent(t *testing.T) {
	store := &stubStore{insertID: 22}
	service := NewService(store)
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 1, Name: "acme", RPMLimit: 10},
		APIKeyID: 2,
	})

	tracker, err := service.BeginRequest(ctx, RequestMetadata{RequestID: "req-2", Stream: true})
	if err != nil {
		t.Fatalf("begin request: %v", err)
	}

	tracker.ObserveStreamChunk(chat.CompletionChunk{
		Model: "gpt-4o-mini",
		Choices: []chat.ChunkChoice{
			{Delta: chat.ChunkDelta{Content: "hello world"}},
		},
	})

	if err := tracker.CompleteStream(context.Background(), chat.CompletionRequest{
		Messages: []chat.Message{{Role: "user", Content: "hello world"}},
	}); err != nil {
		t.Fatalf("complete stream: %v", err)
	}

	if store.updated.Status != "succeeded" || store.updated.Model != "gpt-4o-mini" {
		t.Fatalf("expected succeeded stream update, got %#v", store.updated)
	}

	if store.updated.TotalTokens == 0 {
		t.Fatalf("expected non-zero token usage, got %#v", store.updated)
	}
}

type stubStore struct {
	count    int
	insertID uint64
	inserted UsageRecord
	updated  UsageUpdate
	err      error
}

func (s *stubStore) CountRequestsSince(context.Context, uint64, time.Time) (int, error) {
	return s.count, s.err
}

func (s *stubStore) InsertUsageRecord(_ context.Context, record UsageRecord) (uint64, error) {
	s.inserted = record
	if s.err != nil {
		return 0, s.err
	}
	return s.insertID, nil
}

func (s *stubStore) UpdateUsageRecord(_ context.Context, update UsageUpdate) error {
	s.updated = update
	return s.err
}
