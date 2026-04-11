package oteltracing

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
)

type Config struct {
	ServiceName    string
	TracesEndpoint string
	TracesInsecure bool
	ExportTimeout  time.Duration
}

type ShutdownFunc func(context.Context) error

type endpointConfig struct {
	endpoint string
	path     string
	insecure bool
}

func Configure(ctx context.Context, cfg Config, logger *zap.Logger) (ShutdownFunc, error) {
	endpoint := strings.TrimSpace(cfg.TracesEndpoint)
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		serviceName = "llm-access-gateway"
	}
	timeout := cfg.ExportTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	endpointCfg, err := normalizeEndpoint(endpoint, cfg.TracesInsecure)
	if err != nil {
		return nil, err
	}

	options := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(endpointCfg.endpoint),
		otlptracehttp.WithTimeout(timeout),
	}
	if endpointCfg.path != "" {
		options = append(options, otlptracehttp.WithURLPath(endpointCfg.path))
	}
	if endpointCfg.insecure {
		options = append(options, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("create otlp trace exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes("",
			attribute.String("service.name", serviceName),
			attribute.String("service.namespace", "llm-access-gateway"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create otel resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(timeout), sdktrace.WithExportTimeout(timeout)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	if logger != nil {
		logger.Info("otlp trace exporter enabled",
			zap.String("service_name", serviceName),
			zap.String("endpoint", endpoint),
		)
	}

	return provider.Shutdown, nil
}

func normalizeEndpoint(raw string, insecure bool) (endpointConfig, error) {
	endpoint := strings.TrimSpace(raw)
	if endpoint == "" {
		return endpointConfig{}, fmt.Errorf("otlp traces endpoint is required")
	}

	if !strings.Contains(endpoint, "://") {
		return endpointConfig{
			endpoint: endpoint,
			insecure: insecure,
		}, nil
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpointConfig{}, fmt.Errorf("parse otlp traces endpoint: %w", err)
	}
	if parsed.Host == "" {
		return endpointConfig{}, fmt.Errorf("otlp traces endpoint %q must include a host", raw)
	}

	switch parsed.Scheme {
	case "http":
		return endpointConfig{
			endpoint: parsed.Host,
			path:     parsed.Path,
			insecure: true,
		}, nil
	case "https":
		return endpointConfig{
			endpoint: parsed.Host,
			path:     parsed.Path,
			insecure: false,
		}, nil
	default:
		return endpointConfig{}, fmt.Errorf("otlp traces endpoint scheme %q is not supported", parsed.Scheme)
	}
}
