package config

import "testing"

func TestLoadParsesProviderModelListsFromEnvironment(t *testing.T) {
	t.Setenv("APP_PROVIDER_PRIMARY_MODELS", "[]")
	t.Setenv("APP_PROVIDER_SECONDARY_MODELS", `["gpt-4o-mini","gpt-4.1-mini"]`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if len(cfg.Provider.Primary.Models) != 0 {
		t.Fatalf("expected empty primary models, got %#v", cfg.Provider.Primary.Models)
	}
	if len(cfg.Provider.Secondary.Models) != 2 {
		t.Fatalf("expected two secondary models, got %#v", cfg.Provider.Secondary.Models)
	}
	if cfg.Provider.Secondary.Models[0] != "gpt-4o-mini" || cfg.Provider.Secondary.Models[1] != "gpt-4.1-mini" {
		t.Fatalf("unexpected secondary models %#v", cfg.Provider.Secondary.Models)
	}
}

func TestLoadParsesCommaSeparatedProviderModelListsFromEnvironment(t *testing.T) {
	t.Setenv("APP_PROVIDER_PRIMARY_MODELS", "gpt-4o-mini, gpt-4.1-mini")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if len(cfg.Provider.Primary.Models) != 2 {
		t.Fatalf("expected two primary models, got %#v", cfg.Provider.Primary.Models)
	}
}
