package mysql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/MoChengqian/llm-access-gateway/internal/auth"
)

type AuthStore struct {
	db *sql.DB
}

func NewAuthStore(db *sql.DB) AuthStore {
	return AuthStore{db: db}
}

func (s AuthStore) LookupAPIKey(ctx context.Context, keyHash string) (auth.APIKeyRecord, error) {
	const query = `
SELECT
    ak.id,
    ak.key_hash,
    ak.key_prefix,
    ak.enabled,
    t.id,
    t.name,
    t.enabled,
    t.rpm_limit
FROM api_keys ak
JOIN tenants t ON t.id = ak.tenant_id
WHERE ak.key_hash = ?
LIMIT 1
`

	var record auth.APIKeyRecord
	err := s.db.QueryRowContext(ctx, query, keyHash).Scan(
		&record.APIKeyID,
		&record.APIKeyHash,
		&record.APIKeyPrefix,
		&record.APIKeyEnabled,
		&record.Tenant.ID,
		&record.Tenant.Name,
		&record.TenantEnabled,
		&record.Tenant.RPMLimit,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.APIKeyRecord{}, auth.ErrAPIKeyNotFound
	}
	if err != nil {
		return auth.APIKeyRecord{}, err
	}

	return record, nil
}
