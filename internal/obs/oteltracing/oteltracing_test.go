package oteltracing

import "testing"

func TestNormalizeEndpointAcceptsFullHTTPURL(t *testing.T) {
	cfg, err := normalizeEndpoint("http://otel-collector:4318/v1/traces", false)
	if err != nil {
		t.Fatalf("normalize endpoint: %v", err)
	}

	if cfg.endpoint != "otel-collector:4318" {
		t.Fatalf("expected collector endpoint, got %q", cfg.endpoint)
	}
	if cfg.path != "/v1/traces" {
		t.Fatalf("expected traces path, got %q", cfg.path)
	}
	if !cfg.insecure {
		t.Fatal("expected http endpoint to be insecure")
	}
}

func TestNormalizeEndpointAcceptsHostPortWithInsecureFlag(t *testing.T) {
	cfg, err := normalizeEndpoint("otel-collector:4318", true)
	if err != nil {
		t.Fatalf("normalize endpoint: %v", err)
	}

	if cfg.endpoint != "otel-collector:4318" {
		t.Fatalf("expected host:port endpoint, got %q", cfg.endpoint)
	}
	if cfg.path != "" {
		t.Fatalf("expected default exporter path, got %q", cfg.path)
	}
	if !cfg.insecure {
		t.Fatal("expected insecure flag to be preserved")
	}
}

func TestNormalizeEndpointRejectsUnsupportedScheme(t *testing.T) {
	if _, err := normalizeEndpoint("ftp://otel-collector:4318/v1/traces", false); err == nil {
		t.Fatal("expected unsupported scheme error")
	}
}
