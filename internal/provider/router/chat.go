package router

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
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
	candidates, skipped := p.candidates()
	if len(candidates) == 0 {
		return provider.ChatCompletionResponse{}, errors.New("no provider backends configured")
	}
	p.observeSkipped("create", skipped)

	var lastErr error
	for index, backend := range candidates {
		resp, err := backend.Provider.CreateChatCompletion(ctx, req)
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

	return provider.ChatCompletionResponse{}, lastErr
}

func (p *Provider) StreamChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (<-chan provider.ChatCompletionChunk, error) {
	candidates, skipped := p.candidates()
	if len(candidates) == 0 {
		return nil, errors.New("no provider backends configured")
	}
	p.observeSkipped("stream", skipped)

	var lastErr error
	for index, backend := range candidates {
		chunks, err := backend.Provider.StreamChatCompletion(ctx, req)
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
			return chunks, nil
		}

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

	return nil, lastErr
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
	p.states[name] = backendState{}
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
