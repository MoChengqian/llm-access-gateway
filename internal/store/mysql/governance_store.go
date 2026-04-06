package mysql

import (
	"context"
	"database/sql"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/service/governance"
	usageservice "github.com/MoChengqian/llm-access-gateway/internal/service/usage"
)

type GovernanceStore struct {
	db *sql.DB
}

func NewGovernanceStore(db *sql.DB) GovernanceStore {
	return GovernanceStore{db: db}
}

func (s GovernanceStore) CountRequestsSince(ctx context.Context, tenantID uint64, since time.Time) (int, error) {
	const query = `
SELECT COUNT(*)
FROM request_usages
WHERE tenant_id = ?
  AND created_at >= ?
`

	var count int
	if err := s.db.QueryRowContext(ctx, query, tenantID, since).Scan(&count); err != nil {
		return 0, err
	}

	return count, nil
}

func (s GovernanceStore) SumTotalTokensSince(ctx context.Context, tenantID uint64, since time.Time) (int, error) {
	const query = `
SELECT COALESCE(SUM(total_tokens), 0)
FROM request_usages
WHERE tenant_id = ?
  AND created_at >= ?
`

	var total int
	if err := s.db.QueryRowContext(ctx, query, tenantID, since).Scan(&total); err != nil {
		return 0, err
	}

	return total, nil
}

func (s GovernanceStore) SumTotalTokens(ctx context.Context, tenantID uint64) (int, error) {
	const query = `
SELECT COALESCE(SUM(total_tokens), 0)
FROM request_usages
WHERE tenant_id = ?
`

	var total int
	if err := s.db.QueryRowContext(ctx, query, tenantID).Scan(&total); err != nil {
		return 0, err
	}

	return total, nil
}

func (s GovernanceStore) ListRecentUsageRecords(ctx context.Context, tenantID uint64, limit int) ([]usageservice.RecentUsageRecord, error) {
	const query = `
SELECT
    request_id,
    api_key_id,
    model,
    stream,
    status,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    created_at,
    updated_at
FROM request_usages
WHERE tenant_id = ?
ORDER BY created_at DESC, id DESC
LIMIT ?
`

	rows, err := s.db.QueryContext(ctx, query, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]usageservice.RecentUsageRecord, 0, limit)
	for rows.Next() {
		var record usageservice.RecentUsageRecord
		if err := rows.Scan(
			&record.RequestID,
			&record.APIKeyID,
			&record.Model,
			&record.Stream,
			&record.Status,
			&record.PromptTokens,
			&record.CompletionTokens,
			&record.TotalTokens,
			&record.CreatedAt,
			&record.UpdatedAt,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

func (s GovernanceStore) InsertUsageRecord(ctx context.Context, record governance.UsageRecord) (uint64, error) {
	const statement = `
INSERT INTO request_usages (
    request_id,
    tenant_id,
    api_key_id,
    model,
    stream,
    status,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

	result, err := s.db.ExecContext(
		ctx,
		statement,
		record.RequestID,
		record.TenantID,
		record.APIKeyID,
		record.Model,
		record.Stream,
		record.Status,
		record.PromptTokens,
		record.CompletionTokens,
		record.TotalTokens,
		record.CreatedAt,
	)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	return uint64(id), nil
}

func (s GovernanceStore) UpdateUsageRecord(ctx context.Context, update governance.UsageUpdate) error {
	const statement = `
UPDATE request_usages
SET model = ?,
    status = ?,
    prompt_tokens = ?,
    completion_tokens = ?,
    total_tokens = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
`

	_, err := s.db.ExecContext(
		ctx,
		statement,
		update.Model,
		update.Status,
		update.PromptTokens,
		update.CompletionTokens,
		update.TotalTokens,
		update.ID,
	)
	return err
}
