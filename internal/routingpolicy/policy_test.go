package routingpolicy

import (
	"testing"

	"github.com/MoChengqian/llm-access-gateway/internal/config"
)

func TestDesiredRouteRuleSeedsUsesConfiguredBackends(t *testing.T) {
	rules := DesiredRouteRuleSeeds(config.Config{
		Provider: config.ProviderConfig{
			Backends: []config.ProviderEndpointConfig{
				{
					Type:     "mock",
					Name:     "generic-fallback",
					Priority: 200,
				},
				{
					Type:     "openai",
					Name:     "fast-gpt4o",
					Priority: 10,
					Models:   []string{"gpt-4o-mini", "GPT-4O-MINI"},
				},
			},
		},
	})

	if len(rules) != 2 {
		t.Fatalf("expected 2 route rules, got %#v", rules)
	}
	if rules[0].BackendName != "generic-fallback" || rules[0].Model != "" || rules[0].Priority != 200 {
		t.Fatalf("unexpected generic route rule %#v", rules[0])
	}
	if rules[1].BackendName != "fast-gpt4o" || rules[1].Model != "gpt-4o-mini" || rules[1].Priority != 10 {
		t.Fatalf("unexpected matched route rule %#v", rules[1])
	}
}

func TestDesiredRouteRuleSeedsFallsBackToLegacyProviderNames(t *testing.T) {
	rules := DesiredRouteRuleSeeds(config.Config{
		Provider: config.ProviderConfig{
			Primary: config.ProviderEndpointConfig{
				Type:     "",
				Priority: 100,
			},
			Secondary: config.ProviderEndpointConfig{
				Type:     "openai",
				Priority: 200,
			},
		},
	})

	if len(rules) != 2 {
		t.Fatalf("expected 2 route rules, got %#v", rules)
	}
	if rules[0].BackendName != "mock-primary" {
		t.Fatalf("expected mock-primary, got %#v", rules[0])
	}
	if rules[1].BackendName != "openai-secondary" {
		t.Fatalf("expected openai-secondary, got %#v", rules[1])
	}
}
