package tracing

import (
	"context"
	"testing"

	"go.uber.org/zap"
)

func TestStartRequestSpanAndChildShareTraceID(t *testing.T) {
	rootCtx, rootSpan := StartRequestSpan(context.Background(), zap.NewNop(), "req-123", "http.request")
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
	ctx, span := StartRequestSpan(context.Background(), zap.NewNop(), "", "http.request")
	defer span.End(nil)

	if got := TraceIDFromContext(ctx); got == "" {
		t.Fatal("expected generated trace id")
	}
	if got := SpanIDFromContext(ctx); got == "" {
		t.Fatal("expected generated span id")
	}
}
