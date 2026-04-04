package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/MoChengqian/llm-access-gateway/internal/config"
	mysqlstore "github.com/MoChengqian/llm-access-gateway/internal/store/mysql"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
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

	fmt.Println("development auth seed ready")
	fmt.Printf("tenant=%s\n", seed.TenantName)
	fmt.Printf("api_key=%s\n", seed.APIKey)
	fmt.Println("rpm_limit=60")
	fmt.Println("tpm_limit=4000")
	fmt.Println("token_budget=1000000")
}
