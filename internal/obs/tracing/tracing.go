package tracing

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

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
}

var spanCounter uint64

func StartRequestSpan(ctx context.Context, logger *zap.Logger, requestID string, name string, fields ...zap.Field) (context.Context, *Span) {
	if logger == nil {
		logger = zap.NewNop()
	}

	traceID := requestID
	if traceID == "" {
		traceID = nextID()
	}

	spanID := nextID()
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
			startedAt: time.Now(),
			fields:    append([]zap.Field(nil), fields...),
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
			startedAt:    time.Now(),
			fields:       append([]zap.Field(nil), fields...),
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
	if s == nil || s.logger == nil {
		return
	}

	status := "ok"
	if err != nil {
		status = "error"
		fields = append(fields, zap.String("error", err.Error()))
	}

	fields = append(append([]zap.Field(nil), s.fields...), fields...)
	fields = append(fields,
		zap.String("trace_id", s.traceID),
		zap.String("span_id", s.spanID),
		zap.String("parent_span_id", s.parentSpanID),
		zap.String("request_id", s.requestID),
		zap.String("span_name", s.name),
		zap.String("status", status),
		zap.Duration("duration", time.Since(s.startedAt)),
	)

	s.logger.Info("trace span finished", fields...)
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
