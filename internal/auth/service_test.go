package auth

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestAuthenticateRequestRejectsMissingAPIKey(t *testing.T) {
	service := NewService(stubStore{})

	_, err := service.AuthenticateRequest(context.Background(), "")
	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("expected ErrMissingAPIKey, got %v", err)
	}
}

func TestAuthenticateRequestRejectsInvalidAPIKey(t *testing.T) {
	service := NewService(stubStore{err: ErrAPIKeyNotFound})

	_, err := service.AuthenticateRequest(context.Background(), "Bearer test-key")
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestAuthenticateRequestRejectsInvalidAPIKeyWhenStoreReturnsNoRows(t *testing.T) {
	service := NewService(stubStore{err: sql.ErrNoRows})

	_, err := service.AuthenticateRequest(context.Background(), "Bearer test-key")
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestAuthenticateRequestRejectsDisabledAPIKey(t *testing.T) {
	service := NewService(stubStore{
		record: APIKeyRecord{
			Tenant:        Tenant{ID: 7, Name: "acme"},
			APIKeyEnabled: false,
			TenantEnabled: true,
		},
	})

	_, err := service.AuthenticateRequest(context.Background(), "Bearer test-key")
	if !errors.Is(err, ErrDisabledAPIKey) {
		t.Fatalf("expected ErrDisabledAPIKey, got %v", err)
	}
}

func TestAuthenticateRequestRejectsDisabledTenant(t *testing.T) {
	service := NewService(stubStore{
		record: APIKeyRecord{
			Tenant:        Tenant{ID: 7, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: false,
		},
	})

	_, err := service.AuthenticateRequest(context.Background(), "Bearer test-key")
	if !errors.Is(err, ErrDisabledAPIKey) {
		t.Fatalf("expected ErrDisabledAPIKey, got %v", err)
	}
}

func TestAuthenticateRequestResolvesTenant(t *testing.T) {
	service := NewService(stubStore{
		record: APIKeyRecord{
			Tenant:        Tenant{ID: 7, Name: "acme"},
			APIKeyEnabled: true,
			TenantEnabled: true,
		},
	})

	tenant, err := service.AuthenticateRequest(context.Background(), "Bearer live-key")
	if err != nil {
		t.Fatalf("authenticate request: %v", err)
	}

	if tenant.ID != 7 || tenant.Name != "acme" {
		t.Fatalf("expected tenant acme/7, got %#v", tenant)
	}
}

func TestTenantContextRoundTrip(t *testing.T) {
	ctx := WithTenant(context.Background(), Tenant{ID: 9, Name: "demo"})

	tenant, ok := TenantFromContext(ctx)
	if !ok {
		t.Fatal("expected tenant in context")
	}

	if tenant.ID != 9 || tenant.Name != "demo" {
		t.Fatalf("expected tenant demo/9, got %#v", tenant)
	}
}

type stubStore struct {
	record APIKeyRecord
	err    error
}

func (s stubStore) LookupAPIKey(context.Context, string) (APIKeyRecord, error) {
	if s.err != nil {
		return APIKeyRecord{}, s.err
	}

	return s.record, nil
}
