package models

import (
	"context"
	"time"
)

type Service interface {
	ListModels(ctx context.Context) ListResponse
}

type service struct {
	models []string
}

type ListResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func NewService(models []string) Service {
	unique := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		unique = append(unique, model)
	}

	return service{models: unique}
}

func (s service) ListModels(context.Context) ListResponse {
	items := make([]ModelInfo, 0, len(s.models))
	now := time.Now().Unix()
	for _, model := range s.models {
		items = append(items, ModelInfo{
			ID:      model,
			Object:  "model",
			Created: now,
			OwnedBy: "llm-access-gateway",
		})
	}

	return ListResponse{
		Object: "list",
		Data:   items,
	}
}
