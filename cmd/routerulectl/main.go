package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/MoChengqian/llm-access-gateway/internal/config"
	"github.com/MoChengqian/llm-access-gateway/internal/routingpolicy"
	mysqlstore "github.com/MoChengqian/llm-access-gateway/internal/store/mysql"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: routerulectl <list|replace|sync-from-config>")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.MySQL.DSN == "" {
		return errors.New("APP_MYSQL_DSN is required")
	}

	db, err := sql.Open("mysql", cfg.MySQL.DSN)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	ctx := context.Background()
	if err := mysqlstore.EnsureSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}

	store := mysqlstore.NewRoutingStore(db)
	switch args[0] {
	case "list":
		return listRouteRules(ctx, store)
	case "replace":
		rules, err := parseReplaceArgs(args[1:])
		if err != nil {
			return err
		}
		if err := store.ReplaceRouteRules(ctx, rules); err != nil {
			return fmt.Errorf("replace route rules: %w", err)
		}
		fmt.Printf("route rules replaced count=%d\n", len(rules))
		return listRouteRules(ctx, store)
	case "sync-from-config":
		rules := routingpolicy.DesiredRouteRuleSeeds(cfg)
		if err := store.ReplaceRouteRules(ctx, rules); err != nil {
			return fmt.Errorf("sync route rules from config: %w", err)
		}
		fmt.Printf("route rules synced from config count=%d\n", len(rules))
		return listRouteRules(ctx, store)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func listRouteRules(ctx context.Context, store mysqlstore.RoutingStore) error {
	rules, err := store.ListRouteRules(ctx)
	if err != nil {
		return fmt.Errorf("list route rules: %w", err)
	}
	if len(rules) == 0 {
		fmt.Println("route rules: empty")
		return nil
	}

	for _, rule := range rules {
		fmt.Printf("backend=%s model=%s priority=%d enabled=%t\n", rule.BackendName, rule.Model, rule.Priority, rule.Enabled)
	}
	return nil
}

func parseReplaceArgs(args []string) ([]mysqlstore.RouteRuleRecord, error) {
	flagSet := flag.NewFlagSet("replace", flag.ContinueOnError)
	flagSet.SetOutput(os.Stderr)

	var ruleSpecs multiValueFlag
	flagSet.Var(&ruleSpecs, "rule", "route rule in backend_name,model,priority format; use empty model for generic fallback")
	if err := flagSet.Parse(args); err != nil {
		return nil, fmt.Errorf("parse replace flags: %w", err)
	}
	if len(ruleSpecs) == 0 {
		return nil, errors.New("at least one -rule is required")
	}

	rules := make([]mysqlstore.RouteRuleRecord, 0, len(ruleSpecs))
	for _, spec := range ruleSpecs {
		rule, err := parseRuleSpec(spec)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func parseRuleSpec(spec string) (mysqlstore.RouteRuleRecord, error) {
	parts := strings.Split(spec, ",")
	if len(parts) != 3 {
		return mysqlstore.RouteRuleRecord{}, fmt.Errorf("invalid rule %q: expected backend_name,model,priority", spec)
	}

	backendName := strings.TrimSpace(parts[0])
	if backendName == "" {
		return mysqlstore.RouteRuleRecord{}, fmt.Errorf("invalid rule %q: backend_name is required", spec)
	}

	priority, err := strconv.Atoi(strings.TrimSpace(parts[2]))
	if err != nil {
		return mysqlstore.RouteRuleRecord{}, fmt.Errorf("invalid rule %q: parse priority: %w", spec, err)
	}

	return mysqlstore.RouteRuleRecord{
		BackendName: backendName,
		Model:       strings.TrimSpace(strings.ToLower(parts[1])),
		Priority:    priority,
		Enabled:     true,
	}, nil
}

type multiValueFlag []string

func (f *multiValueFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *multiValueFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}
