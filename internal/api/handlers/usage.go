package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/MoChengqian/llm-access-gateway/internal/service/usage"
)

type UsageService interface {
	GetTenantUsage(ctx context.Context, limit int) (usage.Response, error)
}

type UsageHandler struct {
	service UsageService
}

func NewUsageHandler(service UsageService) UsageHandler {
	return UsageHandler{service: service}
}

func (h UsageHandler) GetUsage(w http.ResponseWriter, r *http.Request) {
	limit, err := usageLimitFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
		return
	}

	resp, err := h.service.GetTenantUsage(r.Context(), limit)
	if err != nil {
		switch {
		case errors.Is(err, usage.ErrInvalidLimit):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func usageLimitFromRequest(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 0, nil
	}

	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if limit <= 0 {
		return 0, usage.ErrInvalidLimit
	}
	return limit, nil
}
