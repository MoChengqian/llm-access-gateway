package router

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/obs/tracing"
	"github.com/MoChengqian/llm-access-gateway/internal/provider"
	"go.uber.org/zap"
)

type Backend struct {
	Name              string
	Provider          provider.ChatCompletionProvider
	Priority          int
	Models            []string
	FirstEventTimeout time.Duration
}

type BackendStatus struct {
	Name                string    `json:"name"`
	Priority            int       `json:"priority"`
	Models              []string  `json:"models,omitempty"`
	Healthy             bool      `json:"healthy"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	UnhealthyUntil      time.Time `json:"unhealthy_until,omitempty"`
	LastProbeAt         time.Time `json:"last_probe_at,omitempty"`
	LastProbeError      string    `json:"last_probe_error,omitempty"`
}

type Config struct {
	FailureThreshold int
	Cooldown         time.Duration
	Observer         Observer
}

type Observer interface {
	OnEvent(Event)
}

type Event struct {
	Type                string
	Operation           string
	Backend             string
	Attempt             int
	Duration            time.Duration
	ConsecutiveFailures int
	UnhealthyUntil      time.Time
	Error               string
}

type Provider struct {
	backends         []Backend
	failureThreshold int
	cooldown         time.Duration
	observer         Observer
	now              func() time.Time

	mu     sync.Mutex
	states map[string]backendState
}

type backendState struct {
	consecutiveFailures int
	unhealthyUntil      time.Time
	lastProbeAt         time.Time
	lastProbeError      string
}

func New(backends []Backend, cfg Config) *Provider {
	threshold := cfg.FailureThreshold
	if threshold <= 0 {
		threshold = 1
	}

	cooldown := cfg.Cooldown
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}

	return &Provider{
		backends:         backends,
		failureThreshold: threshold,
		cooldown:         cooldown,
		observer:         cfg.Observer,
		now:              time.Now,
		states:           make(map[string]backendState, len(backends)),
	}
}

func (p *Provider) CreateChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (provider.ChatCompletionResponse, error) {
	ctx, span := tracing.StartSpan(ctx, "provider.router.create",
		zap.String("model", req.Model),
		zap.Int("configured_backends", len(p.backends)),
	)
	var traceErr error
	defer func() {
		span.End(traceErr)
	}()

	candidates, skipped := p.candidates(req.Model)
	if len(candidates) == 0 {
		traceErr = errors.New("no provider backends configured")
		return provider.ChatCompletionResponse{}, traceErr
	}
	p.observeSkipped("create", skipped)

	var lastErr error
	for index, backend := range candidates {
		startedAt := time.Now()
		attemptCtx, attemptSpan := tracing.StartSpan(ctx, "provider.backend.create",
			zap.String("backend", backend.Name),
			zap.Int("attempt", index+1),
		)
		attemptCtx = provider.WithAttemptBackend(attemptCtx, backend.Name)
		resp, err := backend.Provider.CreateChatCompletion(attemptCtx, req)
		duration := time.Since(startedAt)
		attemptSpan.End(err)
		if err == nil {
			recovered := p.markSuccess(backend.Name)
			p.observe(Event{
				Type:      "provider_request_succeeded",
				Operation: "create",
				Backend:   backend.Name,
				Attempt:   index + 1,
				Duration:  duration,
			})
			if index > 0 {
				p.observe(Event{
					Type:      "provider_fallback_succeeded",
					Operation: "create",
					Backend:   backend.Name,
					Attempt:   index + 1,
					Duration:  duration,
				})
			}
			if recovered {
				p.observe(Event{
					Type:      "provider_recovered",
					Operation: "create",
					Backend:   backend.Name,
					Attempt:   index + 1,
				})
			}
			traceErr = nil
			return resp, nil
		}
		if provider.IsAttemptAccountingError(err) {
			traceErr = err
			return provider.ChatCompletionResponse{}, err
		}

		lastErr = err
		failures, unhealthyUntil := p.markFailure(backend.Name)
		p.observe(Event{
			Type:                "provider_request_failed",
			Operation:           "create",
			Backend:             backend.Name,
			Attempt:             index + 1,
			Duration:            duration,
			ConsecutiveFailures: failures,
			UnhealthyUntil:      unhealthyUntil,
			Error:               err.Error(),
		})
	}

	traceErr = lastErr
	return provider.ChatCompletionResponse{}, lastErr
}

func (p *Provider) StreamChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (<-chan provider.ChatCompletionStreamEvent, error) {
	ctx, span := tracing.StartSpan(ctx, "provider.router.stream",
		zap.String("model", req.Model),
		zap.Int("configured_backends", len(p.backends)),
	)
	candidates, skipped := p.candidates(req.Model)
	if len(candidates) == 0 {
		err := errors.New("no provider backends configured")
		span.End(err)
		return nil, err
	}
	p.observeSkipped("stream", skipped)

	var lastErr error
	for index, backend := range candidates {
		startedAt := time.Now()
		attemptTraceCtx, attemptSpan := tracing.StartSpan(ctx, "provider.backend.stream",
			zap.String("backend", backend.Name),
			zap.Int("attempt", index+1),
		)
		attemptCtx, cancel := context.WithCancel(attemptTraceCtx)
		attemptCtx = provider.WithAttemptBackend(attemptCtx, backend.Name)
		events, err := backend.Provider.StreamChatCompletion(attemptCtx, req)
		if err != nil {
			cancel()
			attemptSpan.End(err)
			if provider.IsAttemptAccountingError(err) {
				span.End(err)
				return nil, err
			}

			lastErr = err
			failures, unhealthyUntil := p.markFailure(backend.Name)
			p.observe(Event{
				Type:                "provider_request_failed",
				Operation:           "stream",
				Backend:             backend.Name,
				Attempt:             index + 1,
				Duration:            time.Since(startedAt),
				ConsecutiveFailures: failures,
				UnhealthyUntil:      unhealthyUntil,
				Error:               err.Error(),
			})
			continue
		}

		firstEvent, err := p.awaitFirstStreamEvent(attemptCtx, events, backend.FirstEventTimeout)
		duration := time.Since(startedAt)
		if err != nil {
			cancel()
			attemptSpan.End(err)

			lastErr = err
			failures, unhealthyUntil := p.markFailure(backend.Name)
			p.observe(Event{
				Type:                "provider_request_failed",
				Operation:           "stream",
				Backend:             backend.Name,
				Attempt:             index + 1,
				Duration:            duration,
				ConsecutiveFailures: failures,
				UnhealthyUntil:      unhealthyUntil,
				Error:               err.Error(),
			})
			continue
		}

		recovered := p.markSuccess(backend.Name)
		p.observe(Event{
			Type:      "provider_request_succeeded",
			Operation: "stream",
			Backend:   backend.Name,
			Attempt:   index + 1,
			Duration:  duration,
		})
		if index > 0 {
			p.observe(Event{
				Type:      "provider_fallback_succeeded",
				Operation: "stream",
				Backend:   backend.Name,
				Attempt:   index + 1,
				Duration:  duration,
			})
		}
		if recovered {
			p.observe(Event{
				Type:      "provider_recovered",
				Operation: "stream",
				Backend:   backend.Name,
				Attempt:   index + 1,
			})
		}
		return p.wrapStream(ctx, events, firstEvent, span, attemptSpan, backend.Name, index+1, cancel), nil
	}

	span.End(lastErr)
	return nil, lastErr
}

func (p *Provider) Probe(ctx context.Context) {
	for _, backend := range p.backends {
		modelProvider, ok := backend.Provider.(provider.ModelProvider)
		if !ok {
			p.observe(Event{
				Type:      "provider_probe_skipped",
				Operation: "probe",
				Backend:   backend.Name,
			})
			continue
		}

		probedAt := p.now()
		startedAt := time.Now()
		_, err := modelProvider.ListModels(ctx)
		duration := time.Since(startedAt)
		if err != nil {
			failures, unhealthyUntil := p.markProbeFailure(backend.Name, err.Error(), probedAt)
			p.observe(Event{
				Type:                "provider_probe_failed",
				Operation:           "probe",
				Backend:             backend.Name,
				Duration:            duration,
				ConsecutiveFailures: failures,
				UnhealthyUntil:      unhealthyUntil,
				Error:               err.Error(),
			})
			continue
		}

		recovered := p.markProbeSuccess(backend.Name, probedAt)
		p.observe(Event{
			Type:      "provider_probe_succeeded",
			Operation: "probe",
			Backend:   backend.Name,
			Duration:  duration,
		})
		if recovered {
			p.observe(Event{
				Type:      "provider_recovered",
				Operation: "probe",
				Backend:   backend.Name,
			})
		}
	}
}

func (p *Provider) wrapStream(ctx context.Context, events <-chan provider.ChatCompletionStreamEvent, first provider.ChatCompletionStreamEvent, routerSpan *tracing.Span, backendSpan *tracing.Span, backend string, attempt int, cancel context.CancelFunc) <-chan provider.ChatCompletionStreamEvent {
	wrapped := make(chan provider.ChatCompletionStreamEvent)

	go func() {
		var traceErr error
		chunkCount := 0
		streamStartedAt := time.Now()
		defer close(wrapped)
		defer cancel()
		defer func() {
			backendSpan.End(traceErr, zap.Int("chunk_count", chunkCount))
			routerSpan.End(traceErr, zap.String("backend", backend), zap.Int("attempt", attempt), zap.Int("chunk_count", chunkCount))
		}()

		if !p.forwardStreamEvent(ctx, wrapped, first, backend, attempt, streamStartedAt, &chunkCount, &traceErr) {
			return
		}

		for {
			select {
			case <-ctx.Done():
				traceErr = ctx.Err()
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if !p.forwardStreamEvent(ctx, wrapped, event, backend, attempt, streamStartedAt, &chunkCount, &traceErr) {
					return
				}
			}
		}
	}()

	return wrapped
}

func (p *Provider) awaitFirstStreamEvent(ctx context.Context, events <-chan provider.ChatCompletionStreamEvent, timeout time.Duration) (provider.ChatCompletionStreamEvent, error) {
	if timeout <= 0 {
		return awaitStreamEvent(ctx, events)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return provider.ChatCompletionStreamEvent{}, ctx.Err()
	case <-timer.C:
		return provider.ChatCompletionStreamEvent{}, context.DeadlineExceeded
	case event, ok := <-events:
		if !ok {
			return provider.ChatCompletionStreamEvent{}, errors.New("upstream stream closed before first chunk")
		}
		if event.Err != nil {
			return provider.ChatCompletionStreamEvent{}, event.Err
		}
		return event, nil
	}
}

func awaitStreamEvent(ctx context.Context, events <-chan provider.ChatCompletionStreamEvent) (provider.ChatCompletionStreamEvent, error) {
	select {
	case <-ctx.Done():
		return provider.ChatCompletionStreamEvent{}, ctx.Err()
	case event, ok := <-events:
		if !ok {
			return provider.ChatCompletionStreamEvent{}, errors.New("upstream stream closed before first chunk")
		}
		if event.Err != nil {
			return provider.ChatCompletionStreamEvent{}, event.Err
		}
		return event, nil
	}
}

func (p *Provider) forwardStreamEvent(ctx context.Context, wrapped chan<- provider.ChatCompletionStreamEvent, event provider.ChatCompletionStreamEvent, backend string, attempt int, streamStartedAt time.Time, chunkCount *int, traceErr *error) bool {
	if event.Err != nil {
		*traceErr = event.Err
		failures, unhealthyUntil := p.markFailure(backend)
		p.observe(Event{
			Type:                "provider_stream_interrupted",
			Operation:           "stream",
			Backend:             backend,
			Attempt:             attempt,
			Duration:            time.Since(streamStartedAt),
			ConsecutiveFailures: failures,
			UnhealthyUntil:      unhealthyUntil,
			Error:               event.Err.Error(),
		})
		select {
		case <-ctx.Done():
		case wrapped <- event:
		}
		return false
	}

	select {
	case <-ctx.Done():
		*traceErr = ctx.Err()
		return false
	case wrapped <- event:
		*chunkCount = *chunkCount + 1
		return true
	}
}

func (p *Provider) candidates(model string) ([]Backend, []Backend) {
	now := p.now()
	ranked := rankBackends(p.backends, model)

	p.mu.Lock()
	defer p.mu.Unlock()

	candidates := make([]Backend, 0, len(ranked))
	skipped := make([]Backend, 0, len(ranked))
	for _, backend := range ranked {
		state := p.states[backend.Name]
		if !state.unhealthyUntil.IsZero() && state.unhealthyUntil.After(now) {
			skipped = append(skipped, backend)
			continue
		}
		candidates = append(candidates, backend)
	}

	if len(candidates) == 0 {
		return append(candidates, skipped...), nil
	}

	return candidates, skipped
}

func (p *Provider) markSuccess(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	previous := p.states[name]
	p.states[name] = backendState{
		lastProbeAt: previous.lastProbeAt,
	}
	return previous.consecutiveFailures > 0 || !previous.unhealthyUntil.IsZero()
}

func (p *Provider) markFailure(name string) (int, time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.states[name]
	state.consecutiveFailures++
	if state.consecutiveFailures >= p.failureThreshold {
		state.unhealthyUntil = p.now().Add(p.cooldown)
	}
	p.states[name] = state
	return state.consecutiveFailures, state.unhealthyUntil
}

func (p *Provider) markProbeSuccess(name string, probedAt time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	previous := p.states[name]
	p.states[name] = backendState{
		lastProbeAt:    probedAt,
		lastProbeError: "",
	}
	return previous.consecutiveFailures > 0 || !previous.unhealthyUntil.IsZero()
}

func (p *Provider) markProbeFailure(name string, probeError string, probedAt time.Time) (int, time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.states[name]
	state.consecutiveFailures++
	state.lastProbeAt = probedAt
	state.lastProbeError = probeError
	if state.consecutiveFailures >= p.failureThreshold {
		state.unhealthyUntil = p.now().Add(p.cooldown)
	}
	p.states[name] = state
	return state.consecutiveFailures, state.unhealthyUntil
}

func (p *Provider) Ready() bool {
	statuses := p.BackendStatuses()
	for _, status := range statuses {
		if status.Healthy {
			return true
		}
	}
	return false
}

func (p *Provider) BackendStatuses() []BackendStatus {
	now := p.now()

	p.mu.Lock()
	defer p.mu.Unlock()

	statuses := make([]BackendStatus, 0, len(p.backends))
	for _, backend := range p.backends {
		state := p.states[backend.Name]
		healthy := state.unhealthyUntil.IsZero() || !state.unhealthyUntil.After(now)
		statuses = append(statuses, BackendStatus{
			Name:                backend.Name,
			Priority:            backend.Priority,
			Models:              append([]string(nil), backend.Models...),
			Healthy:             healthy,
			ConsecutiveFailures: state.consecutiveFailures,
			UnhealthyUntil:      state.unhealthyUntil,
			LastProbeAt:         state.lastProbeAt,
			LastProbeError:      state.lastProbeError,
		})
	}

	return statuses
}

func (p *Provider) observeSkipped(operation string, backends []Backend) {
	for _, backend := range backends {
		p.observe(Event{
			Type:      "provider_skipped_unhealthy",
			Operation: operation,
			Backend:   backend.Name,
		})
	}
}

func (p *Provider) observe(event Event) {
	if p.observer == nil {
		return
	}
	p.observer.OnEvent(event)
}

func rankBackends(backends []Backend, model string) []Backend {
	ranked := append([]Backend(nil), backends...)
	if len(ranked) <= 1 {
		return ranked
	}

	normalizedModel := strings.TrimSpace(strings.ToLower(model))
	sort.SliceStable(ranked, func(i, j int) bool {
		leftScore := backendModelScore(ranked[i], normalizedModel)
		rightScore := backendModelScore(ranked[j], normalizedModel)
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		return ranked[i].Priority < ranked[j].Priority
	})

	return ranked
}

func backendModelScore(backend Backend, normalizedModel string) int {
	if normalizedModel == "" {
		return 1
	}
	if len(backend.Models) == 0 {
		return 1
	}
	for _, candidate := range backend.Models {
		if strings.EqualFold(strings.TrimSpace(candidate), normalizedModel) {
			return 2
		}
	}
	return 0
}
