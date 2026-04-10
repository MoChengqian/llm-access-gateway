package governance

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/auth"
	"github.com/MoChengqian/llm-access-gateway/internal/provider"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
)

var (
	ErrPrincipalNotFound  = errors.New("principal not found")
	ErrRateLimitExceeded  = errors.New("rate limit exceeded")
	ErrTokenLimitExceeded = errors.New("token rate limit exceeded")
	ErrBudgetExceeded     = errors.New("budget exceeded")
	ErrLimiterUnavailable = errors.New("limiter unavailable")
)

type Store interface {
	SumTotalTokens(ctx context.Context, tenantID uint64) (int, error)
	SumTotalAttemptTokens(ctx context.Context, tenantID uint64) (int, error)
	InsertUsageRecord(ctx context.Context, record UsageRecord) (uint64, error)
	UpdateUsageRecord(ctx context.Context, update UsageUpdate) error
	InsertAttemptUsageRecord(ctx context.Context, record AttemptUsageRecord) (uint64, error)
	UpdateAttemptUsageRecord(ctx context.Context, update AttemptUsageUpdate) error
}

type AtomicRequestStore interface {
	BeginRequestAtomic(ctx context.Context, input AtomicBeginRequest) (uint64, error)
}

type AtomicAttemptStore interface {
	BeginAttemptAtomic(ctx context.Context, input AtomicBeginAttempt) (uint64, error)
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
	BindContext(ctx context.Context) context.Context
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

type AttemptUsageRecord struct {
	RequestID        string
	TenantID         uint64
	APIKeyID         uint64
	Backend          string
	Model            string
	Stream           bool
	Status           string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	CreatedAt        time.Time
}

type AttemptUsageUpdate struct {
	ID               uint64
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
	requestID        string
	recordID         uint64
	latestAttemptID  uint64
	latestAttemptTok int
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

type AtomicBeginRequest struct {
	Principal    auth.Principal
	RequestID    string
	Model        string
	Stream       bool
	PromptTokens int
	CreatedAt    time.Time
}

type AtomicBeginAttempt struct {
	Principal    auth.Principal
	RequestID    string
	Backend      string
	Model        string
	Stream       bool
	PromptTokens int
	CreatedAt    time.Time
}

type attemptHandle struct {
	store        Store
	recordID     uint64
	defaultModel string
	promptTokens int
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
	createdAt := s.now()

	if usesAtomicAdmission(s.limiter) {
		return s.beginRequestAtomic(ctx, principal, metadata, estimatedPromptTokens, createdAt)
	}

	if principal.Tenant.TokenBudget > 0 {
		totalTokensUsed, err := s.store.SumTotalAttemptTokens(ctx, principal.Tenant.ID)
		if err != nil {
			return nil, err
		}
		if totalTokensUsed+estimatedPromptTokens > principal.Tenant.TokenBudget {
			return nil, ErrBudgetExceeded
		}
	}

	if s.limiter != nil {
		if err := s.limiter.Admit(ctx, principal, estimatedPromptTokens, createdAt); err != nil {
			if errors.Is(err, ErrLimiterUnavailable) {
				return s.beginRequestAtomic(ctx, principal, metadata, estimatedPromptTokens, createdAt)
			}
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
		CreatedAt:    createdAt,
	})
	if err != nil {
		return nil, err
	}

	return newRequestTracker(s.store, s.limiter, principal, recordID, metadata), nil
}

func (s Service) beginRequestAtomic(ctx context.Context, principal auth.Principal, metadata RequestMetadata, estimatedPromptTokens int, createdAt time.Time) (RequestTracker, error) {
	atomicStore, ok := s.store.(AtomicRequestStore)
	if !ok {
		return nil, errors.New("atomic request store unavailable")
	}

	recordID, err := atomicStore.BeginRequestAtomic(ctx, AtomicBeginRequest{
		Principal:    principal,
		RequestID:    metadata.RequestID,
		Model:        metadata.Model,
		Stream:       metadata.Stream,
		PromptTokens: estimatedPromptTokens,
		CreatedAt:    createdAt,
	})
	if err != nil {
		return nil, err
	}

	return newRequestTracker(s.store, s.limiter, principal, recordID, metadata), nil
}

func newRequestTracker(store Store, limiter Limiter, principal auth.Principal, recordID uint64, metadata RequestMetadata) RequestTracker {
	return &requestTracker{
		store:     store,
		limiter:   limiter,
		principal: principal,
		requestID: metadata.RequestID,
		recordID:  recordID,
		model:     metadata.Model,
		stream:    metadata.Stream,
	}
}

func usesAtomicAdmission(limiter Limiter) bool {
	switch limiter.(type) {
	case MySQLLimiter, *MySQLLimiter:
		return true
	default:
		return false
	}
}

func (t *requestTracker) BindContext(ctx context.Context) context.Context {
	return provider.WithAttemptRecorder(ctx, t)
}

func (t *requestTracker) BeginAttempt(ctx context.Context, metadata provider.AttemptMetadata) (provider.AttemptHandle, error) {
	createdAt := metadata.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	model := strings.TrimSpace(metadata.Model)
	if model == "" {
		model = t.model
	}

	backend := strings.TrimSpace(metadata.Backend)
	if backend == "" {
		backend = provider.AttemptBackendFromContext(ctx)
	}

	recordID, err := t.beginAttempt(ctx, AtomicBeginAttempt{
		Principal:    t.principal,
		RequestID:    t.requestID,
		Backend:      backend,
		Model:        model,
		Stream:       metadata.Stream,
		PromptTokens: metadata.PromptTokens,
		CreatedAt:    createdAt,
	})
	if err != nil {
		return nil, provider.WrapAttemptAccountingError(err)
	}

	if metadata.Stream {
		t.latestAttemptID = recordID
		t.latestAttemptTok = metadata.PromptTokens
	}

	return attemptHandle{
		store:        t.store,
		recordID:     recordID,
		defaultModel: model,
		promptTokens: metadata.PromptTokens,
	}, nil
}

func (t *requestTracker) beginAttempt(ctx context.Context, input AtomicBeginAttempt) (uint64, error) {
	if atomicStore, ok := t.store.(AtomicAttemptStore); ok {
		return atomicStore.BeginAttemptAtomic(ctx, input)
	}

	if input.Principal.Tenant.TokenBudget > 0 {
		totalTokensUsed, err := t.store.SumTotalAttemptTokens(ctx, input.Principal.Tenant.ID)
		if err != nil {
			return 0, err
		}
		if totalTokensUsed+input.PromptTokens > input.Principal.Tenant.TokenBudget {
			return 0, ErrBudgetExceeded
		}
	}

	return t.store.InsertAttemptUsageRecord(ctx, AttemptUsageRecord{
		RequestID:    input.RequestID,
		TenantID:     input.Principal.Tenant.ID,
		APIKeyID:     input.Principal.APIKeyID,
		Backend:      input.Backend,
		Model:        input.Model,
		Stream:       input.Stream,
		Status:       "started",
		PromptTokens: input.PromptTokens,
		TotalTokens:  input.PromptTokens,
		CreatedAt:    input.CreatedAt,
	})
}

func (h attemptHandle) Complete(ctx context.Context, result provider.AttemptResult) error {
	promptTokens := result.PromptTokens
	if promptTokens == 0 {
		promptTokens = h.promptTokens
	}

	totalTokens := result.TotalTokens
	if totalTokens == 0 {
		totalTokens = promptTokens + result.CompletionTokens
	}

	model := strings.TrimSpace(result.Model)
	if model == "" {
		model = h.defaultModel
	}

	return h.store.UpdateAttemptUsageRecord(ctx, AttemptUsageUpdate{
		ID:               h.recordID,
		Model:            model,
		Status:           result.Status,
		PromptTokens:     promptTokens,
		CompletionTokens: result.CompletionTokens,
		TotalTokens:      totalTokens,
	})
}

func (t *requestTracker) Fail(ctx context.Context) error {
	requestErr := t.store.UpdateUsageRecord(ctx, UsageUpdate{
		ID:       t.recordID,
		TenantID: t.principal.Tenant.ID,
		Model:    t.model,
		Status:   "failed",
	})
	if requestErr != nil {
		return requestErr
	}
	if !t.stream || t.latestAttemptID == 0 {
		return nil
	}

	completionTokens := provider.EstimateTextTokens(t.streamCompletion.String())
	return t.store.UpdateAttemptUsageRecord(ctx, AttemptUsageUpdate{
		ID:               t.latestAttemptID,
		Model:            t.model,
		Status:           "failed",
		PromptTokens:     t.latestAttemptTok,
		CompletionTokens: completionTokens,
		TotalTokens:      t.latestAttemptTok + completionTokens,
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
		completionTokens = provider.EstimateTextTokens(joinChoiceContent(resp.Choices))
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
	completionTokens := provider.EstimateTextTokens(t.streamCompletion.String())

	if t.limiter != nil && completionTokens > 0 {
		if err := t.limiter.RecordCompletionTokens(ctx, t.principal, completionTokens, time.Now()); err != nil {
			return err
		}
	}

	requestErr := t.store.UpdateUsageRecord(ctx, UsageUpdate{
		ID:               t.recordID,
		TenantID:         t.principal.Tenant.ID,
		Model:            t.model,
		Status:           "succeeded",
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	})
	if requestErr != nil {
		return requestErr
	}
	if t.latestAttemptID == 0 {
		return nil
	}

	return t.store.UpdateAttemptUsageRecord(ctx, AttemptUsageUpdate{
		ID:               t.latestAttemptID,
		Model:            t.model,
		Status:           "succeeded",
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	})
}

func estimatePromptTokens(messages []chat.Message) int {
	providerMessages := make([]provider.ChatMessage, 0, len(messages))
	for _, message := range messages {
		providerMessages = append(providerMessages, provider.ChatMessage{
			Role:    message.Role,
			Content: message.Content,
		})
	}

	return provider.EstimatePromptTokens(providerMessages)
}

func joinChoiceContent(choices []chat.Choice) string {
	var builder strings.Builder
	for _, choice := range choices {
		builder.WriteString(choice.Message.Content)
	}
	return builder.String()
}
