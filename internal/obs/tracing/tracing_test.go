package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const requestSpanName = "http.request"

func TestStartRequestSpanAndChildShareTraceID(t *testing.T) {
	rootCtx, rootSpan := StartRequestSpan(context.Background(), zap.NewNop(), "req-123", requestSpanName)
	defer rootSpan.End(nil)

	rootTraceID := TraceIDFromContext(rootCtx)
	rootSpanID := SpanIDFromContext(rootCtx)
	if rootTraceID != "req-123" {
		t.Fatalf("expected root trace id req-123, got %s", rootTraceID)
	}
	if rootSpanID == "" {
		t.Fatal("expected root span id to be set")
	}

	childCtx, childSpan := StartSpan(rootCtx, "chat.handler.create_completion")
	defer childSpan.End(nil)

	if got := TraceIDFromContext(childCtx); got != rootTraceID {
		t.Fatalf("expected child trace id %s, got %s", rootTraceID, got)
	}
	if got := SpanIDFromContext(childCtx); got == "" || got == rootSpanID {
		t.Fatalf("expected child span id to differ from root, got %s", got)
	}
}

func TestStartRequestSpanGeneratesTraceIDWhenRequestIDMissing(t *testing.T) {
	ctx, span := StartRequestSpan(context.Background(), zap.NewNop(), "", requestSpanName)
	defer span.End(nil)

	if TraceIDFromContext(ctx) == "" {
		t.Fatal("expected generated trace id")
	}
	if SpanIDFromContext(ctx) == "" {
		t.Fatal("expected generated span id")
	}
}

func TestSpansAreExportedToOpenTelemetryProvider(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previousProvider)
	})

	rootCtx, rootSpan := StartRequestSpan(context.Background(), zap.NewNop(), "req-otel", requestSpanName,
		zap.String("method", "GET"),
	)
	_, childSpan := StartSpan(rootCtx, "provider.backend.create", zap.String("backend", "primary"))

	childSpan.End(nil, zap.Int("attempt", 1))
	rootSpan.End(nil, zap.Int("status", 200))

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("expected two exported spans, got %d", len(spans))
	}

	root := findSpan(spans, requestSpanName)
	if root == nil {
		t.Fatal("expected exported root span")
	}
	if root.SpanKind != oteltrace.SpanKindServer {
		t.Fatalf("expected root span kind server, got %s", root.SpanKind)
	}
	if got := stringAttribute(root.Attributes, "lag.trace_id"); got != "req-otel" {
		t.Fatalf("expected lag trace id req-otel, got %q", got)
	}
	if got := stringAttribute(root.Attributes, "lag.method"); got != "GET" {
		t.Fatalf("expected method attribute GET, got %q", got)
	}

	child := findSpan(spans, "provider.backend.create")
	if child == nil {
		t.Fatal("expected exported child span")
	}
	if child.SpanKind != oteltrace.SpanKindInternal {
		t.Fatalf("expected child span kind internal, got %s", child.SpanKind)
	}
	if !child.Parent.IsValid() {
		t.Fatal("expected child span to keep an OpenTelemetry parent")
	}
	if got := stringAttribute(child.Attributes, "lag.backend"); got != "primary" {
		t.Fatalf("expected backend attribute primary, got %q", got)
	}
}

func findSpan(spans tracetest.SpanStubs, name string) *tracetest.SpanStub {
	for idx := range spans {
		if spans[idx].Name == name {
			return &spans[idx]
		}
	}
	return nil
}

func stringAttribute(attrs []attribute.KeyValue, key string) string {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	return ""
}
