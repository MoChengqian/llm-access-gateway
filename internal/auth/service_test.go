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
			Tenant:        Tenant{ID: 7, Name: "acme", RPMLimit: 60},
			APIKeyEnabled: true,
			TenantEnabled: true,
			APIKeyID:      3,
			APIKeyPrefix:  "lag-live",
		},
	})

	principal, err := service.AuthenticateRequest(context.Background(), "Bearer live-key")
	if err != nil {
		t.Fatalf("authenticate request: %v", err)
	}

	if principal.Tenant.ID != 7 || principal.Tenant.Name != "acme" {
		t.Fatalf("expected tenant acme/7, got %#v", principal)
	}

	if principal.APIKeyID != 3 || principal.APIKeyPrefix != "lag-live" {
		t.Fatalf("expected principal api key metadata, got %#v", principal)
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

func TestPrincipalContextRoundTrip(t *testing.T) {
	ctx := WithPrincipal(context.Background(), Principal{
		Tenant:       Tenant{ID: 9, Name: "demo", RPMLimit: 60},
		APIKeyID:     11,
		APIKeyPrefix: "lag-demo",
	})

	principal, ok := PrincipalFromContext(ctx)
	if !ok {
		t.Fatal("expected principal in context")
	}

	if principal.Tenant.ID != 9 || principal.APIKeyID != 11 {
		t.Fatalf("expected principal round trip, got %#v", principal)
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
