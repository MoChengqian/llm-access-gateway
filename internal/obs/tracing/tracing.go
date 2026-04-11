package tracing

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

type contextKey string

const traceContextKey contextKey = "trace-context"

type spanContext struct {
	traceID   string
	spanID    string
	requestID string
	logger    *zap.Logger
}

type Span struct {
	logger       *zap.Logger
	name         string
	traceID      string
	spanID       string
	parentSpanID string
	requestID    string
	startedAt    time.Time
	fields       []zap.Field
	otelSpan     oteltrace.Span
}

var (
	spanCounter uint64
)

const tracerName = "github.com/MoChengqian/llm-access-gateway/internal/obs/tracing"

func StartRequestSpan(ctx context.Context, logger *zap.Logger, requestID string, name string, fields ...zap.Field) (context.Context, *Span) {
	if logger == nil {
		logger = zap.NewNop()
	}

	traceID := requestID
	if traceID == "" {
		traceID = nextID()
	}

	spanID := nextID()
	startedAt := time.Now()
	ctx, otelSpan := otel.Tracer(tracerName).Start(ctx, name,
		oteltrace.WithSpanKind(oteltrace.SpanKindServer),
		oteltrace.WithTimestamp(startedAt),
		oteltrace.WithAttributes(baseSpanAttributes(name, traceID, spanID, "", requestID)...),
		oteltrace.WithAttributes(zapFieldsToAttributes(fields)...),
	)
	return context.WithValue(ctx, traceContextKey, spanContext{
			traceID:   traceID,
			spanID:    spanID,
			requestID: requestID,
			logger:    logger,
		}), &Span{
			logger:    logger,
			name:      name,
			traceID:   traceID,
			spanID:    spanID,
			requestID: requestID,
			startedAt: startedAt,
			fields:    append([]zap.Field(nil), fields...),
			otelSpan:  otelSpan,
		}
}

func StartSpan(ctx context.Context, name string, fields ...zap.Field) (context.Context, *Span) {
	parent, ok := contextFrom(ctx)
	if !ok {
		return ctx, noopSpan(name)
	}

	spanID := nextID()
	logger := parent.logger
	if logger == nil {
		logger = zap.NewNop()
	}
	startedAt := time.Now()
	ctx, otelSpan := otel.Tracer(tracerName).Start(ctx, name,
		oteltrace.WithSpanKind(oteltrace.SpanKindInternal),
		oteltrace.WithTimestamp(startedAt),
		oteltrace.WithAttributes(baseSpanAttributes(name, parent.traceID, spanID, parent.spanID, parent.requestID)...),
		oteltrace.WithAttributes(zapFieldsToAttributes(fields)...),
	)
	return context.WithValue(ctx, traceContextKey, spanContext{
			traceID:   parent.traceID,
			spanID:    spanID,
			requestID: parent.requestID,
			logger:    logger,
		}), &Span{
			logger:       logger,
			name:         name,
			traceID:      parent.traceID,
			spanID:       spanID,
			parentSpanID: parent.spanID,
			requestID:    parent.requestID,
			startedAt:    startedAt,
			fields:       append([]zap.Field(nil), fields...),
			otelSpan:     otelSpan,
		}
}

func TraceIDFromContext(ctx context.Context) string {
	spanCtx, ok := contextFrom(ctx)
	if !ok {
		return ""
	}
	return spanCtx.traceID
}

func SpanIDFromContext(ctx context.Context) string {
	spanCtx, ok := contextFrom(ctx)
	if !ok {
		return ""
	}
	return spanCtx.spanID
}

func (s *Span) End(err error, fields ...zap.Field) {
	if s == nil {
		return
	}

	endedAt := time.Now()
	duration := endedAt.Sub(s.startedAt)
	status := "ok"
	if err != nil {
		status = "error"
		fields = append(fields, zap.String("error", err.Error()))
	}

	spanFields := append(append([]zap.Field(nil), s.fields...), fields...)
	if s.otelSpan != nil {
		attrs := baseSpanAttributes(s.name, s.traceID, s.spanID, s.parentSpanID, s.requestID)
		attrs = append(attrs,
			attribute.String("lag.span_status", status),
			attribute.Int64("lag.duration_ms", duration.Milliseconds()),
		)
		attrs = append(attrs, zapFieldsToAttributes(spanFields)...)
		s.otelSpan.SetAttributes(attrs...)
		if err != nil {
			s.otelSpan.RecordError(err)
			s.otelSpan.SetStatus(codes.Error, err.Error())
		} else {
			s.otelSpan.SetStatus(codes.Ok, "")
		}
		s.otelSpan.End(oteltrace.WithTimestamp(endedAt))
	}

	logFields := append(spanFields,
		zap.String("trace_id", s.traceID),
		zap.String("span_id", s.spanID),
		zap.String("parent_span_id", s.parentSpanID),
		zap.String("request_id", s.requestID),
		zap.String("span_name", s.name),
		zap.String("status", status),
		zap.Duration("duration", duration),
	)

	if s.logger != nil {
		s.logger.Info("trace span finished", logFields...)
	}
}

func contextFrom(ctx context.Context) (spanContext, bool) {
	spanCtx, ok := ctx.Value(traceContextKey).(spanContext)
	return spanCtx, ok
}

func nextID() string {
	return fmt.Sprintf("%016x", atomic.AddUint64(&spanCounter, 1))
}

func noopSpan(name string) *Span {
	return &Span{
		logger:    zap.NewNop(),
		name:      name,
		traceID:   "",
		spanID:    "",
		requestID: "",
		startedAt: time.Now(),
	}
}

func baseSpanAttributes(name string, traceID string, spanID string, parentSpanID string, requestID string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("lag.trace_id", traceID),
		attribute.String("lag.span_id", spanID),
		attribute.String("lag.parent_span_id", parentSpanID),
		attribute.String("lag.request_id", requestID),
		attribute.String("lag.span_name", name),
	}
}

func zapFieldsToAttributes(fields []zap.Field) []attribute.KeyValue {
	if len(fields) == 0 {
		return nil
	}

	attributes := make([]attribute.KeyValue, 0, len(fields))
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			continue
		}
		key = "lag." + key

		switch value := field.Value.(type) {
		case string:
			attributes = append(attributes, attribute.String(key, value))
		case int:
			attributes = append(attributes, attribute.Int(key, value))
		case int64:
			attributes = append(attributes, attribute.Int64(key, value))
		case uint64:
			attributes = append(attributes, attribute.Int64(key, int64(value)))
		case bool:
			attributes = append(attributes, attribute.Bool(key, value))
		case float64:
			attributes = append(attributes, attribute.Float64(key, value))
		case time.Duration:
			attributes = append(attributes, attribute.Int64(key+"_ms", value.Milliseconds()))
		case error:
			attributes = append(attributes, attribute.String(key, value.Error()))
		default:
			attributes = append(attributes, attribute.String(key, fmt.Sprint(value)))
		}
	}
	return attributes
}
