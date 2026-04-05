package models

import (
	"context"
	"testing"
)

func TestListModelsDeduplicatesAndFormatsResponse(t *testing.T) {
	service := NewService([]string{"gpt-4o-mini", "gpt-4o-mini", "gpt-4.1"})

	resp := service.ListModels(context.Background())
	if resp.Object != "list" {
		t.Fatalf("expected list object, got %s", resp.Object)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 models, got %#v", resp.Data)
	}
	if resp.Data[0].ID != "gpt-4o-mini" || resp.Data[1].ID != "gpt-4.1" {
		t.Fatalf("unexpected models %#v", resp.Data)
	}
	if resp.Data[0].Object != "model" || resp.Data[0].OwnedBy != "llm-access-gateway" {
		t.Fatalf("unexpected model payload %#v", resp.Data[0])
	}
}
