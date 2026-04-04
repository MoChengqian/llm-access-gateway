package governance

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/auth"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
)

var (
	ErrPrincipalNotFound  = errors.New("principal not found")
	ErrRateLimitExceeded  = errors.New("rate limit exceeded")
	ErrTokenLimitExceeded = errors.New("token rate limit exceeded")
	ErrBudgetExceeded     = errors.New("budget exceeded")
)

type Store interface {
	SumTotalTokens(ctx context.Context, tenantID uint64) (int, error)
	InsertUsageRecord(ctx context.Context, record UsageRecord) (uint64, error)
	UpdateUsageRecord(ctx context.Context, update UsageUpdate) error
}

type Limiter interface {
	Admit(ctx context.Context, principal auth.Principal, promptTokens int, now time.Time) error
	RecordCompletionTokens(ctx context.Context, principal auth.Principal, completionTokens int, now time.Time) error
}

type Service struct {
	store   Store
	limiter Limiter
	now     func() time.Time
}

type RequestTracker interface {
	Fail(ctx context.Context) error
	CompleteNonStream(ctx context.Context, req chat.CompletionRequest, resp chat.CompletionResponse) error
	ObserveStreamChunk(chunk chat.CompletionChunk)
	CompleteStream(ctx context.Context, req chat.CompletionRequest) error
}

type UsageRecord struct {
	RequestID        string
	TenantID         uint64
	APIKeyID         uint64
	Model            string
	Stream           bool
	Status           string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CreatedAt        time.Time
}

type UsageUpdate struct {
	ID               uint64
	TenantID         uint64
	Model            string
	Status           string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type requestTracker struct {
	store            Store
	limiter          Limiter
	principal        auth.Principal
	recordID         uint64
	model            string
	stream           bool
	streamCompletion strings.Builder
}

type RequestMetadata struct {
	RequestID string
	Model     string
	Stream    bool
	Messages  []chat.Message
}

func NewService(store Store, limiter Limiter) Service {
	return Service{
		store:   store,
		limiter: limiter,
		now:     time.Now,
	}
}

func (s Service) BeginRequest(ctx context.Context, metadata RequestMetadata) (RequestTracker, error) {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return nil, ErrPrincipalNotFound
	}

	estimatedPromptTokens := estimatePromptTokens(metadata.Messages)

	if principal.Tenant.TokenBudget > 0 {
		totalTokensUsed, err := s.store.SumTotalTokens(ctx, principal.Tenant.ID)
		if err != nil {
			return nil, err
		}
		if totalTokensUsed+estimatedPromptTokens > principal.Tenant.TokenBudget {
			return nil, ErrBudgetExceeded
		}
	}

	if s.limiter != nil {
		if err := s.limiter.Admit(ctx, principal, estimatedPromptTokens, s.now()); err != nil {
			return nil, err
		}
	}

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
	if err != nil {
		return nil, err
	}

	return &requestTracker{
		store:     s.store,
		limiter:   s.limiter,
		principal: principal,
		recordID:  recordID,
		model:     metadata.Model,
		stream:    metadata.Stream,
	}, nil
}

func (t *requestTracker) Fail(ctx context.Context) error {
	return t.store.UpdateUsageRecord(ctx, UsageUpdate{
		ID:       t.recordID,
		TenantID: t.principal.Tenant.ID,
		Model:    t.model,
		Status:   "failed",
	})
}

func (t *requestTracker) CompleteNonStream(ctx context.Context, req chat.CompletionRequest, resp chat.CompletionResponse) error {
	promptTokens := resp.Usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = estimatePromptTokens(req.Messages)
	}
	completionTokens := resp.Usage.CompletionTokens
	totalTokens := resp.Usage.TotalTokens

	if totalTokens == 0 {
		completionTokens = estimateTextTokens(joinChoiceContent(resp.Choices))
		totalTokens = promptTokens + completionTokens
	}

	if t.limiter != nil && completionTokens > 0 {
		if err := t.limiter.RecordCompletionTokens(ctx, t.principal, completionTokens, time.Now()); err != nil {
			return err
		}
	}

	return t.store.UpdateUsageRecord(ctx, UsageUpdate{
		ID:               t.recordID,
		TenantID:         t.principal.Tenant.ID,
		Model:            resp.Model,
		Status:           "succeeded",
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
	})
}

func (t *requestTracker) ObserveStreamChunk(chunk chat.CompletionChunk) {
	if chunk.Model != "" {
		t.model = chunk.Model
	}

	for _, choice := range chunk.Choices {
		t.streamCompletion.WriteString(choice.Delta.Content)
	}
}

func (t *requestTracker) CompleteStream(ctx context.Context, req chat.CompletionRequest) error {
	promptTokens := estimatePromptTokens(req.Messages)
	completionTokens := estimateTextTokens(t.streamCompletion.String())

	if t.limiter != nil && completionTokens > 0 {
		if err := t.limiter.RecordCompletionTokens(ctx, t.principal, completionTokens, time.Now()); err != nil {
			return err
		}
	}

	return t.store.UpdateUsageRecord(ctx, UsageUpdate{
		ID:               t.recordID,
		TenantID:         t.principal.Tenant.ID,
		Model:            t.model,
		Status:           "succeeded",
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	})
}

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

func joinChoiceContent(choices []chat.Choice) string {
	var builder strings.Builder
	for _, choice := range choices {
		builder.WriteString(choice.Message.Content)
	}
	return builder.String()
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
