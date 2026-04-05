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
}

type Provider struct {
	backends         []Backend
	failureThreshold int
	cooldown         time.Duration
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
		now:              time.Now,
		states:           make(map[string]backendState, len(backends)),
	}
}

func (p *Provider) CreateChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (provider.ChatCompletionResponse, error) {
	candidates := p.candidates()
	if len(candidates) == 0 {
		return provider.ChatCompletionResponse{}, errors.New("no provider backends configured")
	}

	var lastErr error
	for _, backend := range candidates {
		resp, err := backend.Provider.CreateChatCompletion(ctx, req)
		if err == nil {
			p.markSuccess(backend.Name)
			return resp, nil
		}

		lastErr = err
		p.markFailure(backend.Name)
	}

	return provider.ChatCompletionResponse{}, lastErr
}

func (p *Provider) StreamChatCompletion(ctx context.Context, req provider.ChatCompletionRequest) (<-chan provider.ChatCompletionChunk, error) {
	candidates := p.candidates()
	if len(candidates) == 0 {
		return nil, errors.New("no provider backends configured")
	}

	var lastErr error
	for _, backend := range candidates {
		chunks, err := backend.Provider.StreamChatCompletion(ctx, req)
		if err == nil {
			p.markSuccess(backend.Name)
			return chunks, nil
		}

		lastErr = err
		p.markFailure(backend.Name)
	}

	return nil, lastErr
}

func (p *Provider) candidates() []Backend {
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
		return append(candidates, skipped...)
	}

	return candidates
}

func (p *Provider) markSuccess(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.states[name] = backendState{}
}

func (p *Provider) markFailure(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.states[name]
	state.consecutiveFailures++
	if state.consecutiveFailures >= p.failureThreshold {
		state.unhealthyUntil = p.now().Add(p.cooldown)
	}
	p.states[name] = state
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
