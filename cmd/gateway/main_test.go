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
		Type: "mock",
		Name: "mock-primary",
	}, "gpt-4o-mini", providermock.Config{})
	if err != nil {
		t.Fatalf("build mock backend: %v", err)
	}

	if backend.Name != "mock-primary" {
		t.Fatalf("expected mock-primary, got %s", backend.Name)
	}
	_, ok := backend.Provider.(provider.ModelProvider)
	if !ok {
		t.Fatal("expected mock backend to implement model listing")
	}
}

func TestBuildProviderBackendSupportsOpenAI(t *testing.T) {
	backend, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type:    "openai",
		Name:    "openai-primary",
		BaseURL: "https://example.com/v1",
		APIKey:  "key",
		Model:   "gpt-4.1-mini",
	}, "gpt-4o-mini", providermock.Config{})
	if err != nil {
		t.Fatalf("build openai backend: %v", err)
	}

	if backend.Name != "openai-primary" {
		t.Fatalf("expected openai-primary, got %s", backend.Name)
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

type stubProber struct {
	done chan struct{}
}

func (s *stubProber) Probe(context.Context) {
	select {
	case s.done <- struct{}{}:
	default:
	}
}
