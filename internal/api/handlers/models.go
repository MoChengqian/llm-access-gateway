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
	writeJSON(w, http.StatusOK, h.service.ListModels(r.Context()))
}
