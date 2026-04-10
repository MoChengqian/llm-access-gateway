package governance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/auth"
	"github.com/MoChengqian/llm-access-gateway/internal/provider"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
)

func TestBeginRequestRejectsWhenPrincipalMissing(t *testing.T) {
	service := NewService(&stubStore{}, &stubLimiter{})

	_, err := service.BeginRequest(context.Background(), RequestMetadata{})
	if !errors.Is(err, ErrPrincipalNotFound) {
		t.Fatalf("expected ErrPrincipalNotFound, got %v", err)
	}
}

func TestBeginRequestRejectsWhenRPMLimitExceeded(t *testing.T) {
	service := NewService(&stubStore{}, &stubLimiter{admitErr: ErrRateLimitExceeded})
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

func TestBeginRequestRejectsWhenTPMLimitExceeded(t *testing.T) {
	service := NewService(&stubStore{}, &stubLimiter{admitErr: ErrTokenLimitExceeded})
	service.now = func() time.Time { return time.Unix(123, 0) }

	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 9, Name: "acme", TPMLimit: 11},
		APIKeyID: 7,
	})

	_, err := service.BeginRequest(ctx, RequestMetadata{
		RequestID: "req-1",
		Messages:  []chat.Message{{Role: "user", Content: "hello world"}},
	})
	if !errors.Is(err, ErrTokenLimitExceeded) {
		t.Fatalf("expected ErrTokenLimitExceeded, got %v", err)
	}
}

func TestBeginRequestRejectsWhenBudgetExceeded(t *testing.T) {
	service := NewService(&stubStore{attemptTokensTotal: 10}, &stubLimiter{})

	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 9, Name: "acme", TokenBudget: 11},
		APIKeyID: 7,
	})

	_, err := service.BeginRequest(ctx, RequestMetadata{
		RequestID: "req-1",
		Messages:  []chat.Message{{Role: "user", Content: "hello world"}},
	})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got %v", err)
	}
}

func TestBeginRequestInsertsStartedUsageRecord(t *testing.T) {
	store := &stubStore{insertID: 11}
	service := NewService(store, &stubLimiter{})
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
		Messages:  []chat.Message{{Role: "user", Content: "hello world"}},
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

	if store.inserted.PromptTokens == 0 || store.inserted.TotalTokens == 0 {
		t.Fatalf("expected prompt token reservation in inserted record, got %#v", store.inserted)
	}
}

func TestCompleteNonStreamUsesProviderUsageWhenPresent(t *testing.T) {
	store := &stubStore{insertID: 21}
	limiter := stubLimiter{}
	service := NewService(store, &limiter)
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 1, Name: "acme", RPMLimit: 10},
		APIKeyID: 2,
	})

	tracker, err := service.BeginRequest(ctx, RequestMetadata{
		RequestID: "req-1",
		Messages:  []chat.Message{{Role: "user", Content: "hello world"}},
	})
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

	if limiter.recordedCompletionTokens != 7 {
		t.Fatalf("expected limiter completion tokens 7, got %#v", limiter)
	}
}

func TestCompleteStreamAggregatesChunkContent(t *testing.T) {
	store := &stubStore{insertID: 22}
	limiter := stubLimiter{}
	service := NewService(store, &limiter)
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 1, Name: "acme", RPMLimit: 10},
		APIKeyID: 2,
	})

	tracker, err := service.BeginRequest(ctx, RequestMetadata{
		RequestID: "req-2",
		Stream:    true,
		Messages:  []chat.Message{{Role: "user", Content: "hello world"}},
	})
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

	if limiter.recordedCompletionTokens == 0 {
		t.Fatalf("expected limiter to record completion tokens, got %#v", limiter)
	}
}

func TestCompleteStreamFinalizesLatestAttemptUsage(t *testing.T) {
	store := &stubStore{insertID: 23}
	limiter := stubLimiter{}
	service := NewService(store, &limiter)
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 1, Name: "acme", RPMLimit: 10},
		APIKeyID: 2,
	})

	tracker, err := service.BeginRequest(ctx, RequestMetadata{
		RequestID: "req-3",
		Stream:    true,
		Messages:  []chat.Message{{Role: "user", Content: "hello world"}},
	})
	if err != nil {
		t.Fatalf("begin request: %v", err)
	}

	recorder := provider.AttemptRecorderFromContext(tracker.BindContext(ctx))
	if recorder == nil {
		t.Fatal("expected attempt recorder in bound context")
	}
	handle, err := recorder.BeginAttempt(ctx, provider.AttemptMetadata{
		Model:        "gpt-4o-mini",
		Stream:       true,
		PromptTokens: 3,
		CreatedAt:    time.Unix(123, 0),
	})
	if err != nil {
		t.Fatalf("begin attempt: %v", err)
	}
	_ = handle

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

	if store.attemptUpdated.Status != "succeeded" || store.attemptUpdated.TotalTokens == 0 {
		t.Fatalf("expected finalized attempt usage, got %#v", store.attemptUpdated)
	}
}

func TestBeginRequestUsesAtomicStoreForMySQLLimiter(t *testing.T) {
	store := &stubAtomicStore{nextID: 30}
	service := NewService(store, NewMySQLLimiter(store))
	service.now = func() time.Time { return time.Unix(123, 0) }

	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 9, Name: "acme", RPMLimit: 5, TPMLimit: 20, TokenBudget: 100},
		APIKeyID: 7,
	})

	tracker, err := service.BeginRequest(ctx, RequestMetadata{
		RequestID: "req-atomic",
		Model:     "gpt-4o-mini",
		Stream:    true,
		Messages:  []chat.Message{{Role: "user", Content: "hello world"}},
	})
	if err != nil {
		t.Fatalf("begin request: %v", err)
	}
	if tracker == nil {
		t.Fatal("expected tracker")
	}
	if store.atomicCalls != 1 {
		t.Fatalf("expected atomic store to be used once, got %d", store.atomicCalls)
	}
	if store.lastAtomicInput.RequestID != "req-atomic" || store.lastAtomicInput.Model != "gpt-4o-mini" {
		t.Fatalf("unexpected atomic input %#v", store.lastAtomicInput)
	}
	if store.inserted.RequestID != "" {
		t.Fatalf("expected non-atomic insert path to be skipped, got %#v", store.inserted)
	}
}

func TestBeginRequestFallsBackToAtomicStoreWhenLimiterUnavailable(t *testing.T) {
	store := &stubAtomicStore{nextID: 40}
	limiter := stubLimiter{admitErr: ErrLimiterUnavailable}
	service := NewService(store, &limiter)
	service.now = func() time.Time { return time.Unix(123, 0) }

	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 9, Name: "acme", RPMLimit: 5, TPMLimit: 20, TokenBudget: 100},
		APIKeyID: 7,
	})

	if _, err := service.BeginRequest(ctx, RequestMetadata{
		RequestID: "req-fallback",
		Messages:  []chat.Message{{Role: "user", Content: "hello world"}},
	}); err != nil {
		t.Fatalf("begin request: %v", err)
	}
	if store.atomicCalls != 1 {
		t.Fatalf("expected atomic fallback to run once, got %d", store.atomicCalls)
	}
	if limiter.admitCalls != 1 {
		t.Fatalf("expected limiter admit to run once, got %d", limiter.admitCalls)
	}
}

func TestAttemptRecorderRejectsExtraAttemptWhenBudgetWouldBeExceeded(t *testing.T) {
	store := &stubStore{}
	service := NewService(store, &stubLimiter{})
	service.now = func() time.Time { return time.Unix(123, 0) }

	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 9, Name: "acme", TokenBudget: 3},
		APIKeyID: 7,
	})

	tracker, err := service.BeginRequest(ctx, RequestMetadata{
		RequestID: "req-budget-attempts",
		Messages:  []chat.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("begin request: %v", err)
	}

	recorder := provider.AttemptRecorderFromContext(tracker.BindContext(ctx))
	if recorder == nil {
		t.Fatal("expected attempt recorder in bound context")
	}

	firstAttempt, err := recorder.BeginAttempt(ctx, provider.AttemptMetadata{
		Model:        "gpt-4o-mini",
		PromptTokens: 2,
		CreatedAt:    time.Unix(123, 0),
	})
	if err != nil {
		t.Fatalf("first attempt: %v", err)
	}
	if err := firstAttempt.Complete(ctx, provider.AttemptResult{
		Model:  "gpt-4o-mini",
		Status: "failed",
	}); err != nil {
		t.Fatalf("complete first attempt: %v", err)
	}

	_, err = recorder.BeginAttempt(ctx, provider.AttemptMetadata{
		Model:        "gpt-4o-mini",
		PromptTokens: 2,
		CreatedAt:    time.Unix(124, 0),
	})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded on extra attempt, got %v", err)
	}
}

func TestBeginRequestAtomicPathClosesConcurrentRPMLimitGap(t *testing.T) {
	store := &stubAtomicStore{nextID: 50}
	service := NewService(store, NewMySQLLimiter(store))
	service.now = func() time.Time { return time.Unix(123, 0) }

	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 9, Name: "acme", RPMLimit: 1},
		APIKeyID: 7,
	})

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := service.BeginRequest(ctx, RequestMetadata{
				RequestID: "req-concurrent",
				Messages:  []chat.Message{{Role: "user", Content: "hello world"}},
			})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	successes := 0
	failures := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrRateLimitExceeded) {
			t.Fatalf("expected ErrRateLimitExceeded, got %v", err)
		}
		failures++
	}

	if successes != 1 || failures != 1 {
		t.Fatalf("expected one success and one rate-limit failure, got successes=%d failures=%d", successes, failures)
	}
}

func TestBeginRequestAtomicPathClosesConcurrentTPMLimitGap(t *testing.T) {
	store := &stubAtomicStore{nextID: 60}
	service := NewService(store, NewMySQLLimiter(store))
	service.now = func() time.Time { return time.Unix(123, 0) }

	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 9, Name: "acme", TPMLimit: 3},
		APIKeyID: 7,
	})

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := service.BeginRequest(ctx, RequestMetadata{
				RequestID: "req-concurrent-tpm",
				Messages:  []chat.Message{{Role: "user", Content: "hello world"}},
			})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	successes := 0
	failures := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrTokenLimitExceeded) {
			t.Fatalf("expected ErrTokenLimitExceeded, got %v", err)
		}
		failures++
	}

	if successes != 1 || failures != 1 {
		t.Fatalf("expected one success and one token-limit failure, got successes=%d failures=%d", successes, failures)
	}
}

func TestBeginRequestAtomicPathClosesConcurrentBudgetGap(t *testing.T) {
	store := &stubAtomicStore{nextID: 70}
	service := NewService(store, NewMySQLLimiter(store))
	service.now = func() time.Time { return time.Unix(123, 0) }

	ctx := auth.WithPrincipal(context.Background(), auth.Principal{
		Tenant:   auth.Tenant{ID: 9, Name: "acme", TokenBudget: 3},
		APIKeyID: 7,
	})

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := service.BeginRequest(ctx, RequestMetadata{
				RequestID: "req-concurrent-budget",
				Messages:  []chat.Message{{Role: "user", Content: "hello world"}},
			})
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)

	successes := 0
	failures := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrBudgetExceeded) {
			t.Fatalf("expected ErrBudgetExceeded, got %v", err)
		}
		failures++
	}

	if successes != 1 || failures != 1 {
		t.Fatalf("expected one success and one budget failure, got successes=%d failures=%d", successes, failures)
	}
}

type stubStore struct {
	tokensTotal         int
	attemptTokensTotal  int
	insertID            uint64
	attemptInsertID     uint64
	inserted            UsageRecord
	attemptInserted     AttemptUsageRecord
	updated             UsageUpdate
	attemptUpdated      AttemptUsageUpdate
	attemptRecordTotals map[uint64]int
	err                 error
}

func (s *stubStore) SumTotalTokens(context.Context, uint64) (int, error) {
	return s.tokensTotal, s.err
}

func (s *stubStore) SumTotalAttemptTokens(context.Context, uint64) (int, error) {
	return s.attemptTokensTotal, s.err
}

func (s *stubStore) CountRequestsSince(context.Context, uint64, time.Time) (int, error) {
	return 0, s.err
}

func (s *stubStore) SumTotalTokensSince(context.Context, uint64, time.Time) (int, error) {
	return 0, s.err
}

func (s *stubStore) InsertUsageRecord(_ context.Context, record UsageRecord) (uint64, error) {
	s.inserted = record
	if s.err != nil {
		return 0, s.err
	}
	return s.insertID, nil
}

func (s *stubStore) InsertAttemptUsageRecord(_ context.Context, record AttemptUsageRecord) (uint64, error) {
	s.attemptInserted = record
	if s.err != nil {
		return 0, s.err
	}
	if s.attemptRecordTotals == nil {
		s.attemptRecordTotals = make(map[uint64]int)
	}
	id := s.attemptInsertID
	if id == 0 {
		id = s.insertID
	}
	if id == 0 {
		id = uint64(len(s.attemptRecordTotals) + 1)
	}
	s.attemptRecordTotals[id] = record.TotalTokens
	s.attemptTokensTotal += record.TotalTokens
	return id, nil
}

func (s *stubStore) UpdateUsageRecord(_ context.Context, update UsageUpdate) error {
	s.updated = update
	return s.err
}

func (s *stubStore) UpdateAttemptUsageRecord(_ context.Context, update AttemptUsageUpdate) error {
	s.attemptUpdated = update
	if s.attemptRecordTotals != nil {
		s.attemptTokensTotal -= s.attemptRecordTotals[update.ID]
		s.attemptRecordTotals[update.ID] = update.TotalTokens
		s.attemptTokensTotal += update.TotalTokens
	}
	return s.err
}

type stubLimiter struct {
	admitErr                 error
	recordErr                error
	recordedCompletionTokens int
	admitCalls               int
}

func (l *stubLimiter) Admit(context.Context, auth.Principal, int, time.Time) error {
	l.admitCalls++
	return l.admitErr
}

func (l *stubLimiter) RecordCompletionTokens(_ context.Context, _ auth.Principal, completionTokens int, _ time.Time) error {
	if l.recordErr != nil {
		return l.recordErr
	}
	l.recordedCompletionTokens += completionTokens
	return nil
}

type stubAtomicStore struct {
	stubStore
	mu                     sync.Mutex
	atomicCalls            int
	lastAtomicInput        AtomicBeginRequest
	attemptAtomicCalls     int
	lastAttemptAtomicInput AtomicBeginAttempt
	nextID                 uint64
	windowRequests         map[string]int
	windowTokens           map[string]int
	attemptTotalTokens     int
}

func (s *stubAtomicStore) BeginRequestAtomic(_ context.Context, input AtomicBeginRequest) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.atomicCalls++
	s.lastAtomicInput = input
	if s.windowRequests == nil {
		s.windowRequests = make(map[string]int)
	}
	if s.windowTokens == nil {
		s.windowTokens = make(map[string]int)
	}

	windowKey := input.CreatedAt.UTC().Format("200601021504")
	if input.Principal.Tenant.TokenBudget > 0 && s.tokensTotal+input.PromptTokens > input.Principal.Tenant.TokenBudget {
		return 0, ErrBudgetExceeded
	}
	if input.Principal.Tenant.RPMLimit > 0 && s.windowRequests[windowKey] >= input.Principal.Tenant.RPMLimit {
		return 0, ErrRateLimitExceeded
	}
	if input.Principal.Tenant.TPMLimit > 0 && s.windowTokens[windowKey]+input.PromptTokens > input.Principal.Tenant.TPMLimit {
		return 0, ErrTokenLimitExceeded
	}

	s.windowRequests[windowKey]++
	s.windowTokens[windowKey] += input.PromptTokens
	s.tokensTotal += input.PromptTokens

	if s.nextID == 0 {
		s.nextID = 1
	}
	id := s.nextID
	s.nextID++
	return id, nil
}

func (s *stubAtomicStore) BeginAttemptAtomic(_ context.Context, input AtomicBeginAttempt) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.attemptAtomicCalls++
	s.lastAttemptAtomicInput = input
	if input.Principal.Tenant.TokenBudget > 0 && s.attemptTotalTokens+input.PromptTokens > input.Principal.Tenant.TokenBudget {
		return 0, ErrBudgetExceeded
	}

	s.attemptTotalTokens += input.PromptTokens
	if s.nextID == 0 {
		s.nextID = 1
	}
	id := s.nextID
	s.nextID++
	return id, nil
}
