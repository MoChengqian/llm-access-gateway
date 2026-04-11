package oteltracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

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

func TestConfigureExportsSpanToConfiguredHTTPPath(t *testing.T) {
	requests := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case requests <- req.Clone(req.Context()):
		default:
		}
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	shutdown, err := Configure(context.Background(), Config{
		ServiceName:    "lag-otlp-test",
		TracesEndpoint: server.URL + "/custom/traces",
		ExportTimeout:  200 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("configure otlp exporter: %v", err)
	}

	ctx, span := otel.Tracer("oteltracing-test").Start(context.Background(), "gateway.request")
	span.End()
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown tracer provider: %v", err)
	}

	select {
	case req := <-requests:
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", req.Method)
		}
		if req.URL.Path != "/custom/traces" {
			t.Fatalf("expected /custom/traces path, got %q", req.URL.Path)
		}
		if req.Header.Get("Content-Type") != "application/x-protobuf" {
			t.Fatalf("unexpected content type %q", req.Header.Get("Content-Type"))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected exported OTLP request")
	}

	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
		t.Fatal("expected Configure to install SDK tracer provider")
	}
	if _, ok := otel.GetTextMapPropagator().(propagation.TraceContext); !ok {
		t.Fatal("expected trace context propagator")
	}
}
