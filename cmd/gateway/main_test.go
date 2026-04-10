package main

import (
	"context"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/config"
	"github.com/MoChengqian/llm-access-gateway/internal/provider"
	providermock "github.com/MoChengqian/llm-access-gateway/internal/provider/mock"
)

func TestBuildProviderBackendSupportsMock(t *testing.T) {
	backend, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type:     "mock",
		Name:     "mock-primary",
		Priority: 90,
		Models:   []string{"gpt-4o-mini", "GPT-4O-MINI"},
	}, "gpt-4o-mini", providermock.Config{})
	if err != nil {
		t.Fatalf("build mock backend: %v", err)
	}

	if backend.Name != "mock-primary" {
		t.Fatalf("expected mock-primary, got %s", backend.Name)
	}
	if backend.Priority != 90 {
		t.Fatalf("expected priority 90, got %d", backend.Priority)
	}
	if len(backend.Models) != 1 || backend.Models[0] != "gpt-4o-mini" {
		t.Fatalf("expected normalized models, got %#v", backend.Models)
	}
	_, ok := backend.Provider.(provider.ModelProvider)
	if !ok {
		t.Fatal("expected mock backend to implement model listing")
	}
}

func TestBuildProviderBackendSupportsOpenAI(t *testing.T) {
	backend, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type:           "openai",
		Name:           "openai-primary",
		BaseURL:        "https://example.com/v1",
		APIKey:         "key",
		Model:          "gpt-4.1-mini",
		Priority:       10,
		Models:         []string{"gpt-4.1-mini"},
		TimeoutSeconds: 7,
	}, "gpt-4o-mini", providermock.Config{})
	if err != nil {
		t.Fatalf("build openai backend: %v", err)
	}

	if backend.Name != "openai-primary" {
		t.Fatalf("expected openai-primary, got %s", backend.Name)
	}
	if backend.Priority != 10 {
		t.Fatalf("expected priority 10, got %d", backend.Priority)
	}
	if backend.FirstEventTimeout != 7*time.Second {
		t.Fatalf("expected first event timeout 7s, got %s", backend.FirstEventTimeout)
	}
	if len(backend.Models) != 1 || backend.Models[0] != "gpt-4.1-mini" {
		t.Fatalf("expected configured models, got %#v", backend.Models)
	}
	if backend.Provider == nil {
		t.Fatal("expected provider implementation")
	}
}

func TestBuildProviderBackendSupportsAnthropic(t *testing.T) {
	backend, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type:           "anthropic",
		Name:           "anthropic-primary",
		BaseURL:        "https://api.anthropic.com/v1",
		APIKey:         "test-key",
		Model:          "claude-3-5-sonnet-latest",
		MaxTokens:      2048,
		Priority:       15,
		Models:         []string{"claude-3-5-sonnet-latest"},
		TimeoutSeconds: 9,
	}, "gpt-4o-mini", providermock.Config{})
	if err != nil {
		t.Fatalf("build anthropic backend: %v", err)
	}

	if backend.Name != "anthropic-primary" {
		t.Fatalf("expected anthropic-primary, got %s", backend.Name)
	}
	if backend.Priority != 15 {
		t.Fatalf("expected priority 15, got %d", backend.Priority)
	}
	if backend.FirstEventTimeout != 9*time.Second {
		t.Fatalf("expected first event timeout 9s, got %s", backend.FirstEventTimeout)
	}
	if len(backend.Models) != 1 || backend.Models[0] != "claude-3-5-sonnet-latest" {
		t.Fatalf("expected configured models, got %#v", backend.Models)
	}
	if backend.Provider == nil {
		t.Fatal("expected provider implementation")
	}
}

func TestBuildProviderBackendSupportsOllama(t *testing.T) {
	backend, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type:           "ollama",
		Name:           "ollama-primary",
		BaseURL:        "http://127.0.0.1:11434",
		Model:          "llama3.1:8b",
		Priority:       20,
		Models:         []string{"llama3.1:8b"},
		TimeoutSeconds: 11,
	}, "gpt-4o-mini", providermock.Config{})
	if err != nil {
		t.Fatalf("build ollama backend: %v", err)
	}

	if backend.Name != "ollama-primary" {
		t.Fatalf("expected ollama-primary, got %s", backend.Name)
	}
	if backend.Priority != 20 {
		t.Fatalf("expected priority 20, got %d", backend.Priority)
	}
	if backend.FirstEventTimeout != 11*time.Second {
		t.Fatalf("expected first event timeout 11s, got %s", backend.FirstEventTimeout)
	}
	if len(backend.Models) != 1 || backend.Models[0] != "llama3.1:8b" {
		t.Fatalf("expected configured models, got %#v", backend.Models)
	}
	if backend.Provider == nil {
		t.Fatal("expected provider implementation")
	}
}

func TestBuildProviderBackendRejectsMissingOpenAIBaseURL(t *testing.T) {
	_, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type: "openai",
	}, "gpt-4o-mini", providermock.Config{})
	if err == nil {
		t.Fatal("expected missing base url error")
	}
}

func TestBuildProviderBackendRejectsMissingAnthropicBaseURL(t *testing.T) {
	_, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type: "anthropic",
	}, "gpt-4o-mini", providermock.Config{})
	if err == nil {
		t.Fatal("expected missing base url error")
	}
}

func TestBuildProviderBackendRejectsMissingOllamaBaseURL(t *testing.T) {
	_, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type: "ollama",
	}, "gpt-4o-mini", providermock.Config{})
	if err == nil {
		t.Fatal("expected missing base url error")
	}
}

func TestBuildProviderBackendsSupportsConfiguredList(t *testing.T) {
	backends, sources, err := buildProviderBackends(config.Config{
		Gateway: config.GatewayConfig{
			DefaultModel: "gpt-4o-mini",
		},
		Provider: config.ProviderConfig{
			Backends: []config.ProviderEndpointConfig{
				{
					Type:     "mock",
					Name:     "generic-fallback",
					Priority: 200,
				},
				{
					Type:     "mock",
					Name:     "fast-gpt4o",
					Priority: 50,
					Models:   []string{"gpt-4o-mini"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("build configured backends: %v", err)
	}

	if len(backends) != 2 {
		t.Fatalf("expected 2 backends, got %d", len(backends))
	}
	if backends[1].Priority != 50 || len(backends[1].Models) != 1 {
		t.Fatalf("expected routing metadata on second backend, got %#v", backends[1])
	}
	if len(sources) != 2 {
		t.Fatalf("expected model sources for both mock backends, got %d", len(sources))
	}
}

func TestStartProviderProbeLoopRunsImmediateProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	prober := &stubProber{done: make(chan struct{}, 1)}
	startProviderProbeLoop(ctx, nil, prober, time.Hour)

	select {
	case <-prober.done:
	case <-time.After(time.Second):
		t.Fatal("expected immediate probe call")
	}
}

func TestGatewayServerTimeoutConfigDefaultsArePositive(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Server.ReadHeaderTimeoutSeconds <= 0 {
		t.Fatalf("expected positive read header timeout, got %d", cfg.Server.ReadHeaderTimeoutSeconds)
	}
	if cfg.Server.ReadTimeoutSeconds <= 0 {
		t.Fatalf("expected positive read timeout, got %d", cfg.Server.ReadTimeoutSeconds)
	}
	if cfg.Server.WriteTimeoutSeconds <= 0 {
		t.Fatalf("expected positive write timeout, got %d", cfg.Server.WriteTimeoutSeconds)
	}
	if cfg.Server.IdleTimeoutSeconds <= 0 {
		t.Fatalf("expected positive idle timeout, got %d", cfg.Server.IdleTimeoutSeconds)
	}
	if cfg.Server.MaxRequestBodyBytes <= 0 {
		t.Fatalf("expected positive max request body bytes, got %d", cfg.Server.MaxRequestBodyBytes)
	}
}

type stubProber struct {
	done chan struct{}
}

func (s *stubProber) Probe(context.Context) {
	select {
	case s.done <- struct{}{}:
	default:
	}
}
