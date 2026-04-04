package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
	"github.com/MoChengqian/llm-access-gateway/internal/service/governance"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

type ChatHandler struct {
	service    chat.Service
	governance governance.Service
}

func NewChatHandler(service chat.Service, governanceService governance.Service) ChatHandler {
	return ChatHandler{
		service:    service,
		governance: governanceService,
	}
}

func (h ChatHandler) CreateCompletion(w http.ResponseWriter, r *http.Request) {
	var req chat.CompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	tracker, err := h.governance.BeginRequest(r.Context(), governance.RequestMetadata{
		RequestID: chimiddleware.GetReqID(r.Context()),
		Model:     req.Model,
		Stream:    req.Stream,
		Messages:  req.Messages,
	})
	if err != nil {
		writeGovernanceError(w, err)
		return
	}

	if req.Stream {
		h.streamCompletion(w, r, req, tracker)
		return
	}

	resp, err := h.service.CreateCompletion(r.Context(), req)
	if err != nil {
		_ = tracker.Fail(r.Context())
		writeServiceError(w, err)
		return
	}

	if err := tracker.CompleteNonStream(r.Context(), req, resp); err != nil {
		writeGovernanceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h ChatHandler) streamCompletion(w http.ResponseWriter, r *http.Request, req chat.CompletionRequest, tracker governance.RequestTracker) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		_ = tracker.Fail(r.Context())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	chunks, err := h.service.StreamCompletion(r.Context(), req)
	if err != nil {
		_ = tracker.Fail(r.Context())
		writeServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	for chunk := range chunks {
		tracker.ObserveStreamChunk(chunk)

		payload, err := json.Marshal(chunk)
		if err != nil {
			_ = tracker.Fail(r.Context())
			return
		}

		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			_ = tracker.Fail(r.Context())
			return
		}
		flusher.Flush()
	}

	if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
		_ = tracker.Fail(r.Context())
		return
	}
	flusher.Flush()

	if err := tracker.CompleteStream(r.Context(), req); err != nil {
		return
	}
}

func writeServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, chat.ErrInvalidRequest) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}

func writeGovernanceError(w http.ResponseWriter, err error) {
	if errors.Is(err, governance.ErrRateLimitExceeded) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		return
	}

	if errors.Is(err, governance.ErrTokenLimitExceeded) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "token rate limit exceeded"})
		return
	}

	if errors.Is(err, governance.ErrBudgetExceeded) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "budget exceeded"})
		return
	}

	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}
