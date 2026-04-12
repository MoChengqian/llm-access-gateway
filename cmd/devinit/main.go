package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"

	"github.com/MoChengqian/llm-access-gateway/internal/config"
	"github.com/MoChengqian/llm-access-gateway/internal/routingpolicy"
	mysqlstore "github.com/MoChengqian/llm-access-gateway/internal/store/mysql"
	// Register the MySQL driver for sql.Open("mysql", ...).
	_ "github.com/go-sql-driver/mysql"
)

const (
	defaultRPMSeedLimit    = 60
	defaultTPMSeedLimit    = 4000
	defaultTokenSeedBudget = 1000000
)

type seedLimits struct {
	RPMLimit    int
	TPMLimit    int
	TokenBudget int
}

func main() {
	limits, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse flags: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	if cfg.MySQL.DSN == "" {
		fmt.Fprintln(os.Stderr, "APP_MYSQL_DSN is required")
		os.Exit(1)
	}

	db, err := sql.Open("mysql", cfg.MySQL.DSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open mysql: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = db.Close()
	}()

	ctx := context.Background()
	seed, err := mysqlstore.SeedDevelopmentData(ctx, db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seed development data: %v\n", err)
		os.Exit(1)
	}
	if err := updateDevelopmentTenantLimits(ctx, db, seed.TenantName, limits); err != nil {
		fmt.Fprintf(os.Stderr, "update tenant limits: %v\n", err)
		os.Exit(1)
	}
	routeRules := routingpolicy.DesiredRouteRuleSeeds(cfg)
	if err := mysqlstore.NewRoutingStore(db).ReplaceRouteRules(ctx, routeRules); err != nil {
		fmt.Fprintf(os.Stderr, "replace route rules: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("development auth seed ready")
	fmt.Printf("tenant=%s\n", seed.TenantName)
	fmt.Printf("api_key=%s\n", seed.APIKey)
	fmt.Printf("rpm_limit=%d\n", limits.RPMLimit)
	fmt.Printf("tpm_limit=%d\n", limits.TPMLimit)
	fmt.Printf("token_budget=%d\n", limits.TokenBudget)
	fmt.Printf("route_rules=%d\n", len(routeRules))
}

func parseFlags(args []string) (seedLimits, error) {
	limits := seedLimits{
		RPMLimit:    defaultRPMSeedLimit,
		TPMLimit:    defaultTPMSeedLimit,
		TokenBudget: defaultTokenSeedBudget,
	}

	flagSet := flag.NewFlagSet("devinit", flag.ContinueOnError)
	flagSet.SetOutput(os.Stderr)
	flagSet.IntVar(&limits.RPMLimit, "rpm-limit", limits.RPMLimit, "tenant requests per minute limit")
	flagSet.IntVar(&limits.TPMLimit, "tpm-limit", limits.TPMLimit, "tenant tokens per minute limit")
	flagSet.IntVar(&limits.TokenBudget, "token-budget", limits.TokenBudget, "tenant total token budget")
	if err := flagSet.Parse(args); err != nil {
		return seedLimits{}, err
	}
	if limits.RPMLimit < 0 {
		return seedLimits{}, fmt.Errorf("-rpm-limit must be >= 0")
	}
	if limits.TPMLimit < 0 {
		return seedLimits{}, fmt.Errorf("-tpm-limit must be >= 0")
	}
	if limits.TokenBudget < 0 {
		return seedLimits{}, fmt.Errorf("-token-budget must be >= 0")
	}

	return limits, nil
}

func updateDevelopmentTenantLimits(ctx context.Context, db *sql.DB, tenantName string, limits seedLimits) error {
	_, err := db.ExecContext(
		ctx,
		`UPDATE tenants
		 SET rpm_limit = ?, tpm_limit = ?, token_budget = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE name = ?`,
		limits.RPMLimit,
		limits.TPMLimit,
		limits.TokenBudget,
		tenantName,
	)
	return err
}
