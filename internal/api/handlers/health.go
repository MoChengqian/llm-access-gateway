package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

type ProviderHealthReader interface {
	Ready() bool
	BackendStatuses() []ProviderBackendStatus
}

type ProviderBackendStatus struct {
	Name                string      `json:"name"`
	Priority            int         `json:"priority"`
	Models              []string    `json:"models,omitempty"`
	RouteRules          []RouteRule `json:"route_rules,omitempty"`
	Healthy             bool        `json:"healthy"`
	ConsecutiveFailures int         `json:"consecutive_failures"`
	UnhealthyUntil      time.Time   `json:"unhealthy_until,omitempty"`
	LastProbeAt         time.Time   `json:"last_probe_at,omitempty"`
	LastProbeError      string      `json:"last_probe_error,omitempty"`
}

type RouteRule struct {
	Model    string `json:"model,omitempty"`
	Priority int    `json:"priority"`
}

type HealthHandler struct {
	providers ProviderHealthReader
}

func NewHealthHandler(providers ProviderHealthReader) HealthHandler {
	return HealthHandler{providers: providers}
}

func (h HealthHandler) Healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h HealthHandler) Readyz(w http.ResponseWriter, _ *http.Request) {
	if h.providers != nil && !h.providers.Ready() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h HealthHandler) Providers(w http.ResponseWriter, _ *http.Request) {
	if h.providers == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ready":     true,
			"providers": []ProviderBackendStatus{},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ready":     h.providers.Ready(),
		"providers": h.providers.BackendStatuses(),
	})
}

func WriteErrorJSON(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(v)
}
