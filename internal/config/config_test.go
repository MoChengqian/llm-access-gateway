package config

import "testing"

const loadConfigErrorFormat = "load config: %v"

func TestLoadParsesProviderModelListsFromEnvironment(t *testing.T) {
	t.Setenv("APP_PROVIDER_PRIMARY_MODELS", "[]")
	t.Setenv("APP_PROVIDER_SECONDARY_MODELS", `["gpt-4o-mini","gpt-4.1-mini"]`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf(loadConfigErrorFormat, err)
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
		t.Fatalf(loadConfigErrorFormat, err)
	}

	if len(cfg.Provider.Primary.Models) != 2 {
		t.Fatalf("expected two primary models, got %#v", cfg.Provider.Primary.Models)
	}
}

func TestLoadParsesProviderMaxTokensFromEnvironment(t *testing.T) {
	t.Setenv("APP_PROVIDER_PRIMARY_MAX_TOKENS", "2048")

	cfg, err := Load()
	if err != nil {
		t.Fatalf(loadConfigErrorFormat, err)
	}

	if cfg.Provider.Primary.MaxTokens != 2048 {
		t.Fatalf("expected primary max tokens 2048, got %d", cfg.Provider.Primary.MaxTokens)
	}
}

func TestLoadParsesObservabilityFromEnvironment(t *testing.T) {
	t.Setenv("APP_OBSERVABILITY_SERVICE_NAME", "lag-test")
	t.Setenv("APP_OBSERVABILITY_OTLP_TRACES_ENDPOINT", "http://otel-collector:4318/v1/traces")
	t.Setenv("APP_OBSERVABILITY_OTLP_TRACES_INSECURE", "true")
	t.Setenv("APP_OBSERVABILITY_OTLP_EXPORT_TIMEOUT_SECONDS", "7")

	cfg, err := Load()
	if err != nil {
		t.Fatalf(loadConfigErrorFormat, err)
	}

	if cfg.Observability.ServiceName != "lag-test" {
		t.Fatalf("expected service name lag-test, got %q", cfg.Observability.ServiceName)
	}
	if cfg.Observability.OTLPTracesEndpoint != "http://otel-collector:4318/v1/traces" {
		t.Fatalf("unexpected traces endpoint %q", cfg.Observability.OTLPTracesEndpoint)
	}
	if !cfg.Observability.OTLPTracesInsecure {
		t.Fatal("expected insecure traces flag")
	}
	if cfg.Observability.OTLPExportTimeoutSeconds != 7 {
		t.Fatalf("expected export timeout 7, got %d", cfg.Observability.OTLPExportTimeoutSeconds)
	}
}
