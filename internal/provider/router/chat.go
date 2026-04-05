package router

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/obs/tracing"
	"github.com/MoChengqian/llm-access-gateway/internal/provider"
	"go.uber.org/zap"
)

type Backend struct {
	Name     string
	Provider provider.ChatCompletionProvider
}

type BackendStatus struct {
	Name                string    `json:"name"`
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

	candidates, skipped := p.candidates()
	if len(candidates) == 0 {
		traceErr = errors.New("no provider backends configured")
		return provider.ChatCompletionResponse{}, traceErr
	}
	p.observeSkipped("create", skipped)

	var lastErr error
	for index, backend := range candidates {
		attemptCtx, attemptSpan := tracing.StartSpan(ctx, "provider.backend.create",
			zap.String("backend", backend.Name),
			zap.Int("attempt", index+1),
		)
		resp, err := backend.Provider.CreateChatCompletion(attemptCtx, req)
		attemptSpan.End(err)
		if err == nil {
			recovered := p.markSuccess(backend.Name)
			if index > 0 {
				p.observe(Event{
					Type:      "provider_fallback_succeeded",
					Operation: "create",
					Backend:   backend.Name,
					Attempt:   index + 1,
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

		lastErr = err
		failures, unhealthyUntil := p.markFailure(backend.Name)
		p.observe(Event{
			Type:                "provider_request_failed",
			Operation:           "create",
			Backend:             backend.Name,
			Attempt:             index + 1,
			ConsecutiveFailures: failures,
			UnhealthyUntil:      unhealthyUntil,
			Error:               err.Error(),
		})
	}

	traceErr = lastErr
	return provider.ChatCompletionResponse{}, lastErr
}

func (p *Provider) StreamChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (<-chan provider.ChatCompletionChunk, error) {
	ctx, span := tracing.StartSpan(ctx, "provider.router.stream",
		zap.String("model", req.Model),
		zap.Int("configured_backends", len(p.backends)),
	)
	candidates, skipped := p.candidates()
	if len(candidates) == 0 {
		err := errors.New("no provider backends configured")
		span.End(err)
		return nil, err
	}
	p.observeSkipped("stream", skipped)

	var lastErr error
	for index, backend := range candidates {
		attemptCtx, attemptSpan := tracing.StartSpan(ctx, "provider.backend.stream",
			zap.String("backend", backend.Name),
			zap.Int("attempt", index+1),
		)
		chunks, err := backend.Provider.StreamChatCompletion(attemptCtx, req)
		if err == nil {
			recovered := p.markSuccess(backend.Name)
			if index > 0 {
				p.observe(Event{
					Type:      "provider_fallback_succeeded",
					Operation: "stream",
					Backend:   backend.Name,
					Attempt:   index + 1,
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
			return p.wrapStream(ctx, chunks, span, attemptSpan, backend.Name, index+1), nil
		}
		attemptSpan.End(err)

		lastErr = err
		failures, unhealthyUntil := p.markFailure(backend.Name)
		p.observe(Event{
			Type:                "provider_request_failed",
			Operation:           "stream",
			Backend:             backend.Name,
			Attempt:             index + 1,
			ConsecutiveFailures: failures,
			UnhealthyUntil:      unhealthyUntil,
			Error:               err.Error(),
		})
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
		_, err := modelProvider.ListModels(ctx)
		if err != nil {
			failures, unhealthyUntil := p.markProbeFailure(backend.Name, err.Error(), probedAt)
			p.observe(Event{
				Type:                "provider_probe_failed",
				Operation:           "probe",
				Backend:             backend.Name,
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

func (p *Provider) wrapStream(ctx context.Context, chunks <-chan provider.ChatCompletionChunk, routerSpan *tracing.Span, backendSpan *tracing.Span, backend string, attempt int) <-chan provider.ChatCompletionChunk {
	wrapped := make(chan provider.ChatCompletionChunk)

	go func() {
		var traceErr error
		chunkCount := 0
		defer close(wrapped)
		defer func() {
			backendSpan.End(traceErr, zap.Int("chunk_count", chunkCount))
			routerSpan.End(traceErr, zap.String("backend", backend), zap.Int("attempt", attempt), zap.Int("chunk_count", chunkCount))
		}()

		for {
			select {
			case <-ctx.Done():
				traceErr = ctx.Err()
				return
			case chunk, ok := <-chunks:
				if !ok {
					return
				}

				select {
				case <-ctx.Done():
					traceErr = ctx.Err()
					return
				case wrapped <- chunk:
					chunkCount++
				}
			}
		}
	}()

	return wrapped
}

func (p *Provider) candidates() ([]Backend, []Backend) {
	now := p.now()

	p.mu.Lock()
	defer p.mu.Unlock()

	candidates := make([]Backend, 0, len(p.backends))
	skipped := make([]Backend, 0, len(p.backends))
	for _, backend := range p.backends {
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
