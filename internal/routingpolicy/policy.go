package routingpolicy

import (
	"fmt"
	"strings"

	"github.com/MoChengqian/llm-access-gateway/internal/config"
	mysqlstore "github.com/MoChengqian/llm-access-gateway/internal/store/mysql"
)

type configuredProviderEndpoint struct {
	role string
	cfg  config.ProviderEndpointConfig
}

func DesiredRouteRuleSeeds(cfg config.Config) []mysqlstore.RouteRuleRecord {
	endpoints := configuredProviderEndpoints(cfg)
	rules := make([]mysqlstore.RouteRuleRecord, 0, len(endpoints))
	for _, endpoint := range endpoints {
		name := resolveProviderName(endpoint.role, endpoint.cfg)
		models := normalizeRouteRuleModels(endpoint.cfg.Models)
		if len(models) == 0 {
			rules = append(rules, mysqlstore.RouteRuleRecord{
				BackendName: name,
				Priority:    endpoint.cfg.Priority,
				Enabled:     true,
			})
			continue
		}

		for _, model := range models {
			rules = append(rules, mysqlstore.RouteRuleRecord{
				BackendName: name,
				Model:       model,
				Priority:    endpoint.cfg.Priority,
				Enabled:     true,
			})
		}
	}
	return rules
}

func configuredProviderEndpoints(cfg config.Config) []configuredProviderEndpoint {
	if len(cfg.Provider.Backends) > 0 {
		endpoints := make([]configuredProviderEndpoint, 0, len(cfg.Provider.Backends))
		for index, providerCfg := range cfg.Provider.Backends {
			endpoints = append(endpoints, configuredProviderEndpoint{
				role: fmt.Sprintf("backends[%d]", index),
				cfg:  providerCfg,
			})
		}
		return endpoints
	}

	return []configuredProviderEndpoint{
		{role: "primary", cfg: cfg.Provider.Primary},
		{role: "secondary", cfg: cfg.Provider.Secondary},
	}
}

func resolveProviderName(role string, cfg config.ProviderEndpointConfig) string {
	name := strings.TrimSpace(cfg.Name)
	if name != "" {
		return name
	}

	providerType := strings.ToLower(strings.TrimSpace(cfg.Type))
	if providerType == "" {
		providerType = "mock"
	}
	return fmt.Sprintf("%s-%s", providerType, role)
}

func normalizeRouteRuleModels(models []string) []string {
	normalized := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		trimmed := strings.TrimSpace(strings.ToLower(model))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}
