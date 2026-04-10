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

func (s GovernanceStore) SumTotalAttemptTokensSince(ctx context.Context, tenantID uint64, since time.Time) (int, error) {
	const query = `
SELECT COALESCE(SUM(total_tokens), 0)
FROM request_attempt_usages
WHERE tenant_id = ?
  AND created_at >= ?
`

	var total int
	if err := s.db.QueryRowContext(ctx, query, tenantID, since).Scan(&total); err != nil {
		return 0, err
	}

	return total, nil
}

func (s GovernanceStore) SumTotalAttemptTokens(ctx context.Context, tenantID uint64) (int, error) {
	const query = `
SELECT COALESCE(SUM(total_tokens), 0)
FROM request_attempt_usages
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

func (s GovernanceStore) BeginRequestAtomic(ctx context.Context, input governance.AtomicBeginRequest) (uint64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := lockTenantForAdmission(ctx, tx, input.Principal.Tenant.ID); err != nil {
		return 0, err
	}

	if input.Principal.Tenant.TokenBudget > 0 {
		totalTokensUsed, err := sumTotalAttemptTokens(ctx, tx, input.Principal.Tenant.ID)
		if err != nil {
			return 0, err
		}
		if totalTokensUsed+input.PromptTokens > input.Principal.Tenant.TokenBudget {
			return 0, governance.ErrBudgetExceeded
		}
	}

	windowStart := input.CreatedAt.Add(-time.Minute)
	if input.Principal.Tenant.RPMLimit > 0 {
		count, err := countRequestsSince(ctx, tx, input.Principal.Tenant.ID, windowStart)
		if err != nil {
			return 0, err
		}
		if count >= input.Principal.Tenant.RPMLimit {
			return 0, governance.ErrRateLimitExceeded
		}
	}

	if input.Principal.Tenant.TPMLimit > 0 {
		tokensUsedThisMinute, err := sumTotalTokensSince(ctx, tx, input.Principal.Tenant.ID, windowStart)
		if err != nil {
			return 0, err
		}
		if tokensUsedThisMinute+input.PromptTokens > input.Principal.Tenant.TPMLimit {
			return 0, governance.ErrTokenLimitExceeded
		}
	}

	result, err := tx.ExecContext(
		ctx,
		insertUsageRecordStatement,
		input.RequestID,
		input.Principal.Tenant.ID,
		input.Principal.APIKeyID,
		input.Model,
		input.Stream,
		"started",
		input.PromptTokens,
		0,
		input.PromptTokens,
		input.CreatedAt,
	)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	committed = true

	return uint64(id), nil
}

func (s GovernanceStore) BeginAttemptAtomic(ctx context.Context, input governance.AtomicBeginAttempt) (uint64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := lockTenantForAdmission(ctx, tx, input.Principal.Tenant.ID); err != nil {
		return 0, err
	}

	if input.Principal.Tenant.TokenBudget > 0 {
		totalTokensUsed, err := sumTotalAttemptTokens(ctx, tx, input.Principal.Tenant.ID)
		if err != nil {
			return 0, err
		}
		if totalTokensUsed+input.PromptTokens > input.Principal.Tenant.TokenBudget {
			return 0, governance.ErrBudgetExceeded
		}
	}

	result, err := tx.ExecContext(
		ctx,
		insertAttemptUsageRecordStatement,
		input.RequestID,
		input.Principal.Tenant.ID,
		input.Principal.APIKeyID,
		input.Backend,
		input.Model,
		input.Stream,
		"started",
		input.PromptTokens,
		0,
		input.PromptTokens,
		input.CreatedAt,
	)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	committed = true

	return uint64(id), nil
}

func (s GovernanceStore) InsertUsageRecord(ctx context.Context, record governance.UsageRecord) (uint64, error) {
	result, err := s.db.ExecContext(
		ctx,
		insertUsageRecordStatement,
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

func (s GovernanceStore) InsertAttemptUsageRecord(ctx context.Context, record governance.AttemptUsageRecord) (uint64, error) {
	result, err := s.db.ExecContext(
		ctx,
		insertAttemptUsageRecordStatement,
		record.RequestID,
		record.TenantID,
		record.APIKeyID,
		record.Backend,
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

func (s GovernanceStore) UpdateAttemptUsageRecord(ctx context.Context, update governance.AttemptUsageUpdate) error {
	const statement = `
UPDATE request_attempt_usages
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

const insertUsageRecordStatement = `
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

const insertAttemptUsageRecordStatement = `
INSERT INTO request_attempt_usages (
    request_id,
    tenant_id,
    api_key_id,
    backend,
    model,
    stream,
    status,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

func lockTenantForAdmission(ctx context.Context, tx *sql.Tx, tenantID uint64) (uint64, error) {
	const query = `
SELECT id
FROM tenants
WHERE id = ?
FOR UPDATE
`

	var lockedTenantID uint64
	if err := tx.QueryRowContext(ctx, query, tenantID).Scan(&lockedTenantID); err != nil {
		return 0, err
	}
	return lockedTenantID, nil
}

func countRequestsSince(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, tenantID uint64, since time.Time) (int, error) {
	const query = `
SELECT COUNT(*)
FROM request_usages
WHERE tenant_id = ?
  AND created_at >= ?
`

	var count int
	if err := querier.QueryRowContext(ctx, query, tenantID, since).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func sumTotalTokensSince(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, tenantID uint64, since time.Time) (int, error) {
	const query = `
SELECT COALESCE(SUM(total_tokens), 0)
FROM request_usages
WHERE tenant_id = ?
  AND created_at >= ?
`

	var total int
	if err := querier.QueryRowContext(ctx, query, tenantID, since).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func sumTotalTokens(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, tenantID uint64) (int, error) {
	const query = `
SELECT COALESCE(SUM(total_tokens), 0)
FROM request_usages
WHERE tenant_id = ?
`

	var total int
	if err := querier.QueryRowContext(ctx, query, tenantID).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func sumTotalAttemptTokens(ctx context.Context, querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, tenantID uint64) (int, error) {
	const query = `
SELECT COALESCE(SUM(total_tokens), 0)
FROM request_attempt_usages
WHERE tenant_id = ?
`

	var total int
	if err := querier.QueryRowContext(ctx, query, tenantID).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}
