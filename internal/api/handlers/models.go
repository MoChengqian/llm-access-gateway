package handlers

import (
	"net/http"

	"github.com/MoChengqian/llm-access-gateway/internal/service/models"
)

type ModelsHandler struct {
	service models.Service
}

func NewModelsHandler(service models.Service) ModelsHandler {
	return ModelsHandler{service: service}
}

func (h ModelsHandler) List(w http.ResponseWriter, r *http.Request) {
	resp, err := h.service.ListModels(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
