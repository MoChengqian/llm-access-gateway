package mysql

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitialMigrationContainsRuntimeSchemaStatements(t *testing.T) {
	migrationPath := filepath.Join("..", "..", "..", "migrations", "001_init.sql")
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	normalizedMigration := normalizeSQL(string(content))
	runtimeStatements := []string{
		createTenantsTableSQL,
		createAPIKeysTableSQL,
		createRequestUsagesTableSQL,
		createRequestAttemptUsagesTableSQL,
		createRouteRulesTableSQL,
	}

	for _, statement := range runtimeStatements {
		normalizedStatement := normalizeSQL(statement)
		if !strings.Contains(normalizedMigration, normalizedStatement) {
			t.Fatalf("migration %s missing runtime schema statement:\n%s", migrationPath, statement)
		}
	}
}

func normalizeSQL(input string) string {
	replacer := strings.NewReplacer("\n", " ", "\t", " ", ";", " ")
	return strings.Join(strings.Fields(replacer.Replace(input)), " ")
}
