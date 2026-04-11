package main

import (
	"context"
	"testing"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/config"
	"github.com/MoChengqian/llm-access-gateway/internal/provider"
	providermock "github.com/MoChengqian/llm-access-gateway/internal/provider/mock"
	providerrouter "github.com/MoChengqian/llm-access-gateway/internal/provider/router"
	mysqlstore "github.com/MoChengqian/llm-access-gateway/internal/store/mysql"
)

const (
	testDefaultModel   = "gpt-4o-mini"
	testGenericBackend = "generic-fallback"
	testFastBackend    = "fast-gpt4o"
)

func TestBuildProviderBackendSupportsMock(t *testing.T) {
	backend, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type:     "mock",
		Name:     "mock-primary",
		Priority: 90,
		Models:   []string{testDefaultModel, "GPT-4O-MINI"},
	}, testDefaultModel, providermock.Config{})
	if err != nil {
		t.Fatalf("build mock backend: %v", err)
	}

	if backend.Name != "mock-primary" {
		t.Fatalf("expected mock-primary, got %s", backend.Name)
	}
	if backend.Priority != 90 {
		t.Fatalf("expected priority 90, got %d", backend.Priority)
	}
	if len(backend.Models) != 1 || backend.Models[0] != testDefaultModel {
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
	}, testDefaultModel, providermock.Config{})
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
	}, testDefaultModel, providermock.Config{})
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
	}, testDefaultModel, providermock.Config{})
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
	}, testDefaultModel, providermock.Config{})
	if err == nil {
		t.Fatal("expected missing base url error")
	}
}

func TestBuildProviderBackendRejectsMissingAnthropicBaseURL(t *testing.T) {
	_, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type: "anthropic",
	}, testDefaultModel, providermock.Config{})
	if err == nil {
		t.Fatal("expected missing base url error")
	}
}

func TestBuildProviderBackendRejectsMissingOllamaBaseURL(t *testing.T) {
	_, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type: "ollama",
	}, testDefaultModel, providermock.Config{})
	if err == nil {
		t.Fatal("expected missing base url error")
	}
}

func TestBuildProviderBackendsSupportsConfiguredList(t *testing.T) {
	backends, sources, err := buildProviderBackends(config.Config{
		Gateway: config.GatewayConfig{
			DefaultModel: testDefaultModel,
		},
		Provider: config.ProviderConfig{
			Backends: []config.ProviderEndpointConfig{
				{
					Type:     "mock",
					Name:     testGenericBackend,
					Priority: 200,
				},
				{
					Type:     "mock",
					Name:     testFastBackend,
					Priority: 50,
					Models:   []string{testDefaultModel},
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

func TestApplyRouteRulesFiltersBackendsAndOverridesRoutingMetadata(t *testing.T) {
	backends := []providerrouter.Backend{
		{Name: testGenericBackend, Priority: 200, Models: []string{"legacy-model"}, Provider: providermock.New()},
		{Name: testFastBackend, Priority: 50, Models: []string{testDefaultModel}, Provider: providermock.New()},
		{Name: "unused", Priority: 10, Provider: providermock.New()},
	}

	routed, enabled, err := applyRouteRules(backends, []mysqlstore.RouteRuleRecord{
		{BackendName: testFastBackend, Model: testDefaultModel, Priority: 10},
		{BackendName: testGenericBackend, Priority: 20},
	})
	if err != nil {
		t.Fatalf("apply route rules: %v", err)
	}
	if !enabled {
		t.Fatal("expected route rules to be enabled")
	}
	if len(routed) != 2 {
		t.Fatalf("expected 2 routed backends, got %#v", routed)
	}
	if routed[0].Name != testGenericBackend || routed[0].Priority != 20 || len(routed[0].Models) != 0 || len(routed[0].RouteRules) != 1 {
		t.Fatalf("expected generic backend to be rewritten with route rule metadata, got %#v", routed[0])
	}
	if routed[1].Name != testFastBackend || routed[1].Priority != 10 || len(routed[1].RouteRules) != 1 {
		t.Fatalf("expected matched backend to be rewritten with route rule metadata, got %#v", routed[1])
	}
}

func TestApplyRouteRulesRejectsUnknownBackend(t *testing.T) {
	_, _, err := applyRouteRules([]providerrouter.Backend{
		{Name: "known", Provider: providermock.New()},
	}, []mysqlstore.RouteRuleRecord{
		{BackendName: "unknown", Priority: 10},
	})
	if err == nil {
		t.Fatalf("expected unknown backend error, got %v", err)
	}
	if got := err.Error(); got != `route rule references unknown backend "unknown"` {
		t.Fatalf("unexpected error %q", got)
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
