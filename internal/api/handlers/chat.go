package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/obs/tracing"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
	"github.com/MoChengqian/llm-access-gateway/internal/service/governance"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

const internalServerErrorMessage = "internal server error"

type ChatHandler struct {
	service    chat.Service
	governance governance.Service
	metrics    MetricsRecorder
}

type MetricsRecorder interface {
	RecordGovernanceRejection(reason string)
	RecordStreamRequest(ttft time.Duration)
	RecordStreamChunk()
}

func NewChatHandler(service chat.Service, governanceService governance.Service, metricsRecorder MetricsRecorder) ChatHandler {
	return ChatHandler{
		service:    service,
		governance: governanceService,
		metrics:    metricsRecorder,
	}
}

func (h ChatHandler) CreateCompletion(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracing.StartSpan(r.Context(), "chat.handler.create_completion")
	var req chat.CompletionRequest
	var traceErr error
	defer func() {
		span.End(traceErr,
			zap.String("model", req.Model),
			zap.String("stream", strconv.FormatBool(req.Stream)),
		)
	}()

	if err := decodeCompletionRequest(w, r, &req); err != nil {
		traceErr = err
		return
	}

	tracker, err := h.beginTrackedRequest(ctx, req)
	if err != nil {
		traceErr = err
		writeGovernanceErrorWithMetrics(w, err, h.metrics)
		return
	}

	if req.Stream {
		h.streamCompletion(w, r.WithContext(ctx), req, tracker)
		return
	}

	if err := h.handleNonStreamCompletion(w, ctx, req, tracker); err != nil {
		traceErr = err
	}
}

func (h ChatHandler) beginTrackedRequest(ctx context.Context, req chat.CompletionRequest) (governance.RequestTracker, error) {
	return h.governance.BeginRequest(ctx, governance.RequestMetadata{
		RequestID: chimiddleware.GetReqID(ctx),
		Model:     req.Model,
		Stream:    req.Stream,
		Messages:  req.Messages,
	})
}

func (h ChatHandler) handleNonStreamCompletion(w http.ResponseWriter, ctx context.Context, req chat.CompletionRequest, tracker governance.RequestTracker) error {
	resp, err := h.service.CreateCompletion(tracker.BindContext(ctx), req)
	if err != nil {
		_ = tracker.Fail(ctx)
		if isGovernanceError(err) {
			writeGovernanceErrorWithMetrics(w, err, h.metrics)
			return err
		}
		writeServiceError(w, err)
		return err
	}

	if err := tracker.CompleteNonStream(ctx, req, resp); err != nil {
		writeGovernanceErrorWithMetrics(w, err, h.metrics)
		return err
	}

	writeJSON(w, http.StatusOK, resp)
	return nil
}

func decodeCompletionRequest(w http.ResponseWriter, r *http.Request, req *chat.CompletionRequest) error {
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return err
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return err
	}
	return nil
}

func (h ChatHandler) streamCompletion(w http.ResponseWriter, r *http.Request, req chat.CompletionRequest, tracker governance.RequestTracker) {
	ctx, span := tracing.StartSpan(r.Context(), "chat.handler.stream_completion",
		zap.String("model", req.Model),
		zap.String("stream", "true"),
	)
	var traceErr error
	startedAt := time.Now()
	firstChunkWritten := false
	defer func() {
		span.End(traceErr)
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		traceErr = errors.New("streaming unsupported")
		_ = tracker.Fail(ctx)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	events, err := h.service.StreamCompletion(tracker.BindContext(ctx), req)
	if err != nil {
		traceErr = err
		_ = tracker.Fail(ctx)
		if isGovernanceError(err) {
			writeGovernanceErrorWithMetrics(w, err, h.metrics)
			return
		}
		writeServiceError(w, err)
		return
	}

	headersWritten := false

	for event := range events {
		if event.Err != nil {
			traceErr = event.Err
			_ = tracker.Fail(ctx)
			if !headersWritten {
				if isGovernanceError(event.Err) {
					writeGovernanceErrorWithMetrics(w, event.Err, h.metrics)
					return
				}
				writeServiceError(w, event.Err)
			}
			return
		}

		chunk := event.Chunk
		if !headersWritten {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			w.WriteHeader(http.StatusOK)
			headersWritten = true
		}

		tracker.ObserveStreamChunk(chunk)

		payload, err := json.Marshal(chunk)
		if err != nil {
			traceErr = err
			_ = tracker.Fail(ctx)
			return
		}

		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			traceErr = err
			_ = tracker.Fail(ctx)
			return
		}
		if h.metrics != nil {
			if !firstChunkWritten {
				h.metrics.RecordStreamRequest(time.Since(startedAt))
				firstChunkWritten = true
			}
			h.metrics.RecordStreamChunk()
		}
		flusher.Flush()
	}

	if !headersWritten {
		traceErr = errors.New("stream ended before first chunk")
		_ = tracker.Fail(ctx)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": internalServerErrorMessage})
		return
	}

	if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
		traceErr = err
		_ = tracker.Fail(ctx)
		return
	}
	flusher.Flush()

	if err := tracker.CompleteStream(ctx, req); err != nil {
		traceErr = err
		return
	}
}

func writeServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, chat.ErrInvalidRequest) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": internalServerErrorMessage})
}

func writeGovernanceError(w http.ResponseWriter, err error) {
	writeGovernanceErrorWithMetrics(w, err, nil)
}

func writeGovernanceErrorWithMetrics(w http.ResponseWriter, err error, metrics MetricsRecorder) {
	if errors.Is(err, governance.ErrRateLimitExceeded) {
		recordGovernanceRejection(metrics, "rate_limit_exceeded")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return
	}

	if errors.Is(err, governance.ErrTokenLimitExceeded) {
		recordGovernanceRejection(metrics, "token_limit_exceeded")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "token rate limit exceeded"})
		return
	}

	if errors.Is(err, governance.ErrBudgetExceeded) {
		recordGovernanceRejection(metrics, "budget_exceeded")
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "budget exceeded"})
		return
	}

	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": internalServerErrorMessage})
}

func recordGovernanceRejection(metrics MetricsRecorder, reason string) {
	if metrics == nil {
		return
	}
	metrics.RecordGovernanceRejection(reason)
}

func isGovernanceError(err error) bool {
	return errors.Is(err, governance.ErrRateLimitExceeded) ||
		errors.Is(err, governance.ErrTokenLimitExceeded) ||
		errors.Is(err, governance.ErrBudgetExceeded)
}
