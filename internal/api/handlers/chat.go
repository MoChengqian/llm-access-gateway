package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
)

type ChatHandler struct {
	service chat.Service
}

func NewChatHandler(service chat.Service) ChatHandler {
	return ChatHandler{service: service}
}

func (h ChatHandler) CreateCompletion(w http.ResponseWriter, r *http.Request) {
	var req chat.CompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Stream {
		h.streamCompletion(w, r, req)
		return
	}

	resp, err := h.service.CreateCompletion(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h ChatHandler) streamCompletion(w http.ResponseWriter, r *http.Request, req chat.CompletionRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	chunks, err := h.service.StreamCompletion(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	for chunk := range chunks {
		payload, err := json.Marshal(chunk)
		if err != nil {
			return
		}

		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return
		}
		flusher.Flush()
	}

	if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
		return
	}
	flusher.Flush()
}

func writeServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, chat.ErrInvalidRequest) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}
