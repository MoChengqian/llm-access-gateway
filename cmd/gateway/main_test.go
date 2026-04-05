package main

import (
	"testing"

	"github.com/MoChengqian/llm-access-gateway/internal/config"
	providermock "github.com/MoChengqian/llm-access-gateway/internal/provider/mock"
)

func TestBuildProviderBackendSupportsMock(t *testing.T) {
	backend, model, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type: "mock",
		Name: "mock-primary",
	}, "gpt-4o-mini", providermock.Config{})
	if err != nil {
		t.Fatalf("build mock backend: %v", err)
	}

	if backend.Name != "mock-primary" {
		t.Fatalf("expected mock-primary, got %s", backend.Name)
	}
	if model != "gpt-4o-mini" {
		t.Fatalf("expected model gpt-4o-mini, got %s", model)
	}
}

func TestBuildProviderBackendSupportsOpenAI(t *testing.T) {
	backend, model, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
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
	if model != "gpt-4.1-mini" {
		t.Fatalf("expected model gpt-4.1-mini, got %s", model)
	}
}

func TestBuildProviderBackendRejectsMissingOpenAIBaseURL(t *testing.T) {
	_, _, err := buildProviderBackend("primary", config.ProviderEndpointConfig{
		Type: "openai",
	}, "gpt-4o-mini", providermock.Config{})
	if err == nil {
		t.Fatal("expected missing base url error")
	}
}

func TestCollectModelsDeduplicates(t *testing.T) {
	models := collectModels("gpt-4o-mini", "", "gpt-4.1", "gpt-4o-mini")
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %#v", models)
	}
}
