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
	CountRequestsSince(ctx context.Context, tenantID uint64, since time.Time) (int, error)
	SumTotalTokensSince(ctx context.Context, tenantID uint64, since time.Time) (int, error)
	SumTotalTokens(ctx context.Context, tenantID uint64) (int, error)
	InsertUsageRecord(ctx context.Context, record UsageRecord) (uint64, error)
	UpdateUsageRecord(ctx context.Context, update UsageUpdate) error
}

type Service struct {
	store Store
	now   func() time.Time
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
	Model            string
	Status           string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type requestTracker struct {
	store            Store
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

func NewService(store Store) Service {
	return Service{
		store: store,
		now:   time.Now,
	}
}

func (s Service) BeginRequest(ctx context.Context, metadata RequestMetadata) (RequestTracker, error) {
	principal, ok := auth.PrincipalFromContext(ctx)
	if !ok {
		return nil, ErrPrincipalNotFound
	}

	if principal.Tenant.RPMLimit > 0 {
		count, err := s.store.CountRequestsSince(ctx, principal.Tenant.ID, s.now().Add(-time.Minute))
		if err != nil {
			return nil, err
		}
		if count >= principal.Tenant.RPMLimit {
			return nil, ErrRateLimitExceeded
		}
	}

	estimatedPromptTokens := estimatePromptTokens(metadata.Messages)
	if principal.Tenant.TPMLimit > 0 {
		tokensUsedThisMinute, err := s.store.SumTotalTokensSince(ctx, principal.Tenant.ID, s.now().Add(-time.Minute))
		if err != nil {
			return nil, err
		}
		if tokensUsedThisMinute+estimatedPromptTokens > principal.Tenant.TPMLimit {
			return nil, ErrTokenLimitExceeded
		}
	}

	if principal.Tenant.TokenBudget > 0 {
		totalTokensUsed, err := s.store.SumTotalTokens(ctx, principal.Tenant.ID)
		if err != nil {
			return nil, err
		}
		if totalTokensUsed+estimatedPromptTokens > principal.Tenant.TokenBudget {
			return nil, ErrBudgetExceeded
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
		store:    s.store,
		recordID: recordID,
		model:    metadata.Model,
		stream:   metadata.Stream,
	}, nil
}

func (t *requestTracker) Fail(ctx context.Context) error {
	return t.store.UpdateUsageRecord(ctx, UsageUpdate{
		ID:     t.recordID,
		Model:  t.model,
		Status: "failed",
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

	return t.store.UpdateUsageRecord(ctx, UsageUpdate{
		ID:               t.recordID,
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

	return t.store.UpdateUsageRecord(ctx, UsageUpdate{
		ID:               t.recordID,
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
