package mysql

import (
	"context"
	"database/sql"
	"strings"
)

type RouteRuleRecord struct {
	BackendName string
	Model       string
	Priority    int
	Enabled     bool
}

type RoutingStore struct {
	db *sql.DB
}

func NewRoutingStore(db *sql.DB) RoutingStore {
	return RoutingStore{db: db}
}

func (s RoutingStore) ListRouteRules(ctx context.Context) ([]RouteRuleRecord, error) {
	exists, err := tableExists(ctx, s.db, "route_rules")
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	const query = `
SELECT backend_name, model, priority, enabled
FROM route_rules
WHERE enabled = TRUE
ORDER BY priority ASC, backend_name ASC, model ASC
`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := make([]RouteRuleRecord, 0)
	for rows.Next() {
		var rule RouteRuleRecord
		if err := rows.Scan(&rule.BackendName, &rule.Model, &rule.Priority, &rule.Enabled); err != nil {
			return nil, err
		}
		rule.BackendName = strings.TrimSpace(rule.BackendName)
		rule.Model = strings.TrimSpace(strings.ToLower(rule.Model))
		rules = append(rules, rule)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return rules, nil
}

func (s RoutingStore) ReplaceRouteRules(ctx context.Context, rules []RouteRuleRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM route_rules`); err != nil {
		return err
	}

	for _, rule := range rules {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO route_rules (backend_name, model, priority, enabled)
			 VALUES (?, ?, ?, TRUE)`,
			strings.TrimSpace(rule.BackendName),
			strings.TrimSpace(strings.ToLower(rule.Model)),
			rule.Priority,
		); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func tableExists(ctx context.Context, db *sql.DB, tableName string) (bool, error) {
	const query = `
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = DATABASE()
  AND table_name = ?
`

	var count int
	if err := db.QueryRowContext(ctx, query, tableName).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
