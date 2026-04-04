package mysql

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
)

const (
	createTenantsTableSQL = `
CREATE TABLE IF NOT EXISTS tenants (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name VARCHAR(255) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    rpm_limit INT NOT NULL DEFAULT 60,
    tpm_limit INT NOT NULL DEFAULT 4000,
    token_budget INT NOT NULL DEFAULT 1000000,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_tenants_name (name)
)`

	createAPIKeysTableSQL = `
CREATE TABLE IF NOT EXISTS api_keys (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    key_hash CHAR(64) NOT NULL,
    key_prefix VARCHAR(32) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_api_keys_key_hash (key_hash),
    KEY idx_api_keys_tenant_id (tenant_id),
    CONSTRAINT fk_api_keys_tenant_id
        FOREIGN KEY (tenant_id) REFERENCES tenants (id)
        ON DELETE RESTRICT
        ON UPDATE RESTRICT
)`

	createRequestUsagesTableSQL = `
CREATE TABLE IF NOT EXISTS request_usages (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    request_id VARCHAR(255) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    api_key_id BIGINT UNSIGNED NOT NULL,
    model VARCHAR(255) NOT NULL DEFAULT '',
    stream BOOLEAN NOT NULL DEFAULT FALSE,
    status VARCHAR(16) NOT NULL,
    prompt_tokens INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    total_tokens INT NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_request_usages_tenant_created_at (tenant_id, created_at),
    KEY idx_request_usages_request_id (request_id),
    CONSTRAINT fk_request_usages_tenant_id
        FOREIGN KEY (tenant_id) REFERENCES tenants (id)
        ON DELETE RESTRICT
        ON UPDATE RESTRICT,
    CONSTRAINT fk_request_usages_api_key_id
        FOREIGN KEY (api_key_id) REFERENCES api_keys (id)
        ON DELETE RESTRICT
        ON UPDATE RESTRICT
)`

	developmentTenantName = "local-dev"
	developmentAPIKey     = "lag-local-dev-key"
)

var errTenantNotFound = errors.New("tenant not found after seed")

type DevelopmentSeed struct {
	TenantName string
	APIKey     string
}

func EnsureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		createTenantsTableSQL,
		createAPIKeysTableSQL,
		createRequestUsagesTableSQL,
		`ALTER TABLE tenants ADD COLUMN IF NOT EXISTS tpm_limit INT NOT NULL DEFAULT 4000 AFTER rpm_limit`,
		`ALTER TABLE tenants ADD COLUMN IF NOT EXISTS token_budget INT NOT NULL DEFAULT 1000000 AFTER tpm_limit`,
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	return nil
}

func SeedDevelopmentData(ctx context.Context, db *sql.DB) (DevelopmentSeed, error) {
	if err := EnsureSchema(ctx, db); err != nil {
		return DevelopmentSeed{}, err
	}

	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO tenants (name, enabled, rpm_limit, tpm_limit, token_budget) VALUES (?, TRUE, 60, 4000, 1000000)
		 ON DUPLICATE KEY UPDATE enabled = VALUES(enabled), rpm_limit = VALUES(rpm_limit), tpm_limit = VALUES(tpm_limit), token_budget = VALUES(token_budget), updated_at = CURRENT_TIMESTAMP`,
		developmentTenantName,
	); err != nil {
		return DevelopmentSeed{}, err
	}

	var tenantID uint64
	if err := db.QueryRowContext(
		ctx,
		`SELECT id FROM tenants WHERE name = ? LIMIT 1`,
		developmentTenantName,
	).Scan(&tenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DevelopmentSeed{}, errTenantNotFound
		}
		return DevelopmentSeed{}, err
	}

	keyHash := hashAPIKey(developmentAPIKey)
	keyPrefix := apiKeyPrefix(developmentAPIKey)
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO api_keys (tenant_id, key_hash, key_prefix, enabled)
		 VALUES (?, ?, ?, TRUE)
		 ON DUPLICATE KEY UPDATE tenant_id = VALUES(tenant_id), enabled = VALUES(enabled), updated_at = CURRENT_TIMESTAMP`,
		tenantID,
		keyHash,
		keyPrefix,
	); err != nil {
		return DevelopmentSeed{}, err
	}

	return DevelopmentSeed{
		TenantName: developmentTenantName,
		APIKey:     developmentAPIKey,
	}, nil
}

func hashAPIKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

func apiKeyPrefix(rawKey string) string {
	const maxPrefixLen = 12
	prefix := strings.TrimSpace(rawKey)
	if len(prefix) > maxPrefixLen {
		prefix = prefix[:maxPrefixLen]
	}
	return prefix
}
