package models

import (
	"context"
	"sort"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
)

type Service interface {
	ListModels(ctx context.Context) (ListResponse, error)
}

type Source interface {
	ListModels(ctx context.Context) ([]provider.Model, error)
}

type service struct {
	sources []Source
}

type ListResponse struct {
	Object string           `json:"object"`
	Data   []provider.Model `json:"data"`
}

func NewService(sources []Source) Service {
	copied := make([]Source, 0, len(sources))
	for _, source := range sources {
		if source == nil {
			continue
		}
		copied = append(copied, source)
	}

	return service{sources: copied}
}

func (s service) ListModels(ctx context.Context) (ListResponse, error) {
	merged := make(map[string]provider.Model)
	for _, source := range s.sources {
		models, err := source.ListModels(ctx)
		if err != nil {
			return ListResponse{}, err
		}
		for _, model := range models {
			if model.ID == "" {
				continue
			}
			if model.Object == "" {
				model.Object = "model"
			}
			merged[model.ID] = model
		}
	}

	data := make([]provider.Model, 0, len(merged))
	for _, model := range merged {
		data = append(data, model)
	}
	sort.Slice(data, func(i, j int) bool {
		return data[i].ID < data[j].ID
	})

	return ListResponse{
		Object: "list",
		Data:   data,
	}, nil
}
