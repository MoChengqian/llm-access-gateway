package models

import (
	"context"
	"errors"
	"testing"

	"github.com/MoChengqian/llm-access-gateway/internal/provider"
)

func TestListModelsDeduplicatesAndFormatsResponse(t *testing.T) {
	service := NewService([]Source{
		stubSource{models: []provider.Model{
			{ID: "gpt-4o-mini", Object: "model", Created: 1, OwnedBy: "primary"},
			{ID: "gpt-4.1", Object: "model", Created: 2, OwnedBy: "primary"},
		}},
		stubSource{models: []provider.Model{
			{ID: "gpt-4o-mini", Object: "model", Created: 3, OwnedBy: "secondary"},
		}},
	})

	resp, err := service.ListModels(context.Background())
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if resp.Object != "list" {
		t.Fatalf("expected list object, got %s", resp.Object)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 models, got %#v", resp.Data)
	}
	if resp.Data[0].ID != "gpt-4.1" || resp.Data[1].ID != "gpt-4o-mini" {
		t.Fatalf("unexpected models %#v", resp.Data)
	}
	if resp.Data[1].Object != "model" || resp.Data[1].OwnedBy != "secondary" {
		t.Fatalf("unexpected model payload %#v", resp.Data[1])
	}
}

func TestListModelsReturnsSourceError(t *testing.T) {
	service := NewService([]Source{
		stubSource{err: errors.New("boom")},
	})

	_, err := service.ListModels(context.Background())
	if err == nil {
		t.Fatal("expected source error")
	}
}

type stubSource struct {
	models []provider.Model
	err    error
}

func (s stubSource) ListModels(context.Context) ([]provider.Model, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.models, nil
}
