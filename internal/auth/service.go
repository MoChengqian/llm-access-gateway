package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
)

var (
	ErrMissingAPIKey  = errors.New("missing api key")
	ErrInvalidAPIKey  = errors.New("invalid api key")
	ErrDisabledAPIKey = errors.New("disabled api key")
	ErrAPIKeyNotFound = errors.New("api key not found")
)

type contextKey string

const (
	tenantContextKey    contextKey = "tenant"
	principalContextKey contextKey = "principal"
)

type Tenant struct {
	ID          uint64
	Name        string
	RPMLimit    int
	TPMLimit    int
	TokenBudget int
}

type Principal struct {
	Tenant       Tenant
	APIKeyID     uint64
	APIKeyPrefix string
}

type APIKeyRecord struct {
	Tenant        Tenant
	APIKeyEnabled bool
	TenantEnabled bool
	APIKeyID      uint64
	APIKeyHash    string
	APIKeyPrefix  string
}

type APIKeyStore interface {
	LookupAPIKey(ctx context.Context, keyHash string) (APIKeyRecord, error)
}

type Authenticator interface {
	AuthenticateRequest(ctx context.Context, authorization string) (Principal, error)
}

type Service struct {
	store APIKeyStore
}

func NewService(store APIKeyStore) Service {
	return Service{store: store}
}

func (s Service) AuthenticateRequest(ctx context.Context, authorization string) (Principal, error) {
	rawKey, err := bearerToken(authorization)
	if err != nil {
		return Principal{}, err
	}

	record, err := s.store.LookupAPIKey(ctx, hashAPIKey(rawKey))
	if err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) || errors.Is(err, sql.ErrNoRows) {
			return Principal{}, ErrInvalidAPIKey
		}

		return Principal{}, err
	}

	if !record.APIKeyEnabled || !record.TenantEnabled {
		return Principal{}, ErrDisabledAPIKey
	}

	return Principal{
		Tenant:       record.Tenant,
		APIKeyID:     record.APIKeyID,
		APIKeyPrefix: record.APIKeyPrefix,
	}, nil
}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	ctx = context.WithValue(ctx, principalContextKey, principal)
	return context.WithValue(ctx, tenantContextKey, principal.Tenant)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey).(Principal)
	return principal, ok
}

func WithTenant(ctx context.Context, tenant Tenant) context.Context {
	return context.WithValue(ctx, tenantContextKey, tenant)
}

func TenantFromContext(ctx context.Context) (Tenant, bool) {
	tenant, ok := ctx.Value(tenantContextKey).(Tenant)
	return tenant, ok
}

func hashAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

func bearerToken(authorization string) (string, error) {
	if strings.TrimSpace(authorization) == "" {
		return "", ErrMissingAPIKey
	}

	parts := strings.SplitN(strings.TrimSpace(authorization), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", ErrInvalidAPIKey
	}

	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", ErrInvalidAPIKey
	}

	return token, nil
}
