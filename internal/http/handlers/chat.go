package handlers

import (
	"encoding/json"
	"errors"
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

	resp, err := h.service.CreateCompletion(r.Context(), req)
	if err != nil {
		if errors.Is(err, chat.ErrInvalidRequest) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
