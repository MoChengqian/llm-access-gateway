package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/api"
	"github.com/MoChengqian/llm-access-gateway/internal/api/handlers"
	"github.com/MoChengqian/llm-access-gateway/internal/auth"
	"github.com/MoChengqian/llm-access-gateway/internal/config"
	"github.com/MoChengqian/llm-access-gateway/internal/obs/metrics"
	"github.com/MoChengqian/llm-access-gateway/internal/obs/oteltracing"
	provideranthropic "github.com/MoChengqian/llm-access-gateway/internal/provider/anthropic"
	providermock "github.com/MoChengqian/llm-access-gateway/internal/provider/mock"
	providerollama "github.com/MoChengqian/llm-access-gateway/internal/provider/ollama"
	provideropenai "github.com/MoChengqian/llm-access-gateway/internal/provider/openai"
	providerrouter "github.com/MoChengqian/llm-access-gateway/internal/provider/router"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
	"github.com/MoChengqian/llm-access-gateway/internal/service/governance"
	modelsservice "github.com/MoChengqian/llm-access-gateway/internal/service/models"
	usageservice "github.com/MoChengqian/llm-access-gateway/internal/service/usage"
	mysqlstore "github.com/MoChengqian/llm-access-gateway/internal/store/mysql"
	redisstore "github.com/MoChengqian/llm-access-gateway/internal/store/redis"
	// Register the MySQL driver for sql.Open("mysql", ...).
	_ "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
)

type gatewayApp struct {
	authService       auth.Service
	governanceService governance.Service
	usageService      usageservice.Service
	chatService       chat.Service
	modelsService     modelsservice.Service
	chatProvider      *providerrouter.Provider
	metricsRegistry   *metrics.Registry
}

func main() {
	cfg := mustLoadConfig()
	logger := mustBuildLogger(cfg)
	defer syncLogger(logger)

	traceShutdown := mustConfigureTracing(cfg, logger)
	defer shutdownTracing(traceShutdown, logger)

	db := mustOpenDatabase(cfg, logger)
	defer closeDatabase(db)

	app := mustBuildGatewayApp(cfg, db, logger)
	server := newGatewayServer(cfg, logger, app)
	runGatewayServer(cfg, logger, server, app.chatProvider)
}

func mustLoadConfig() config.Config {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	return cfg
}

func mustBuildLogger(cfg config.Config) *zap.Logger {
	loggerConfig := zap.NewProductionConfig()
	loggerConfig.Level = cfg.Log.Level

	logger, err := loggerConfig.Build()
	if err != nil {
		panic(err)
	}
	return logger
}

func syncLogger(logger *zap.Logger) {
	if logger == nil {
		return
	}
	_ = logger.Sync()
}

func mustConfigureTracing(cfg config.Config, logger *zap.Logger) func(context.Context) error {
	traceShutdown, err := oteltracing.Configure(context.Background(), oteltracing.Config{
		ServiceName:    cfg.Observability.ServiceName,
		TracesEndpoint: cfg.Observability.OTLPTracesEndpoint,
		TracesInsecure: cfg.Observability.OTLPTracesInsecure,
		ExportTimeout:  time.Duration(cfg.Observability.OTLPExportTimeoutSeconds) * time.Second,
	}, logger)
	if err != nil {
		logger.Fatal("otel tracing setup failed", zap.Error(err))
	}
	return traceShutdown
}

func shutdownTracing(traceShutdown func(context.Context) error, logger *zap.Logger) {
	if traceShutdown == nil {
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := traceShutdown(shutdownCtx); err != nil {
		logger.Error("otel tracing shutdown failed", zap.Error(err))
	}
}

func mustOpenDatabase(cfg config.Config, logger *zap.Logger) *sql.DB {
	if cfg.MySQL.DSN == "" {
		logger.Fatal("mysql dsn is required", zap.String("field", "mysql.dsn"))
	}

	db, err := sql.Open("mysql", cfg.MySQL.DSN)
	if err != nil {
		logger.Fatal("mysql open failed", zap.Error(err))
	}
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		logger.Fatal("mysql ping failed", zap.Error(err))
	}
	return db
}

func closeDatabase(db *sql.DB) {
	if db == nil {
		return
	}
	_ = db.Close()
}

func mustBuildGatewayApp(cfg config.Config, db *sql.DB, logger *zap.Logger) gatewayApp {
	authService := auth.NewService(mysqlstore.NewAuthStore(db))
	governanceStore := mysqlstore.NewGovernanceStore(db)
	limiter := buildLimiter(cfg, governanceStore, logger)
	metricsRegistry := metrics.NewRegistry()
	backends := mustLoadProviderBackends(cfg, db, logger)
	chatProvider := providerrouter.New(backends, providerrouter.Config{
		FailureThreshold: cfg.Gateway.ProviderFailureThreshold,
		Cooldown:         time.Duration(cfg.Gateway.ProviderCooldownSeconds) * time.Second,
		Observer: multiProviderObserver{
			observers: []providerrouter.Observer{
				providerEventLogger{logger: logger},
				metricsRegistry,
			},
		},
	})

	return gatewayApp{
		authService:       authService,
		governanceService: governance.NewService(governanceStore, limiter),
		usageService:      usageservice.NewService(governanceStore),
		chatService:       chat.NewService(cfg.Gateway.DefaultModel, chatProvider),
		modelsService:     modelsservice.NewService(collectModelSources(backends)),
		chatProvider:      chatProvider,
		metricsRegistry:   metricsRegistry,
	}
}

func buildLimiter(cfg config.Config, governanceStore mysqlstore.GovernanceStore, logger *zap.Logger) governance.Limiter {
	limiter := governance.Limiter(governance.NewMySQLLimiter(governanceStore))
	if cfg.Redis.Address == "" {
		return limiter
	}

	redisClient := redisstore.NewClient(redisstore.Config{
		Address:  cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	if err := redisClient.Ping(context.Background()); err != nil {
		logger.Error("redis ping failed, falling back to mysql limiter", zap.Error(err))
		return limiter
	}

	logger.Info("redis limiter enabled", zap.String("address", cfg.Redis.Address), zap.Int("db", cfg.Redis.DB))
	return governance.NewRedisLimiter(redisClient, limiter)
}

func mustLoadProviderBackends(cfg config.Config, db *sql.DB, logger *zap.Logger) []providerrouter.Backend {
	backends, _, err := buildProviderBackends(cfg)
	if err != nil {
		logger.Fatal("provider setup failed", zap.Error(err))
	}

	routeRules, err := mysqlstore.NewRoutingStore(db).ListRouteRules(context.Background())
	if err != nil {
		logger.Fatal("route rule load failed", zap.Error(err))
	}

	backends, routeRulesEnabled, err := applyRouteRules(backends, routeRules)
	if err != nil {
		logger.Fatal("route rule setup failed", zap.Error(err))
	}
	if routeRulesEnabled {
		logger.Info("database route rules enabled",
			zap.Int("route_rule_count", len(routeRules)),
			zap.Int("routed_backend_count", len(backends)),
		)
	}
	return backends
}

func newGatewayServer(cfg config.Config, logger *zap.Logger, app gatewayApp) *http.Server {
	return &http.Server{
		Addr: cfg.Server.Address,
		Handler: api.NewRouter(api.RouterDependencies{
			Logger:              logger,
			ChatService:         app.chatService,
			ModelsService:       app.modelsService,
			UsageService:        app.usageService,
			Authenticator:       app.authService,
			GovernanceService:   app.governanceService,
			Providers:           providerHealthAdapter{provider: app.chatProvider},
			MetricsHandler:      app.metricsRegistry,
			MetricsRecorder:     app.metricsRegistry,
			MaxRequestBodyBytes: cfg.Server.MaxRequestBodyBytes,
		}),
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeoutSeconds) * time.Second,
		ReadTimeout:       time.Duration(cfg.Server.ReadTimeoutSeconds) * time.Second,
		WriteTimeout:      time.Duration(cfg.Server.WriteTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.Server.IdleTimeoutSeconds) * time.Second,
	}
}

func runGatewayServer(cfg config.Config, logger *zap.Logger, server *http.Server, chatProvider *providerrouter.Provider) {
	probeCtx, stopProbe := context.WithCancel(context.Background())
	defer stopProbe()
	if cfg.Gateway.ProviderProbeIntervalSeconds > 0 {
		startProviderProbeLoop(probeCtx, logger, chatProvider, time.Duration(cfg.Gateway.ProviderProbeIntervalSeconds)*time.Second)
	}

	go func() {
		logger.Info("gateway starting", zap.String("address", cfg.Server.Address))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("gateway stopped unexpectedly", zap.Error(err))
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()
	logger.Info("gateway shutting down")
	stopProbe()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	}
}

func startProviderProbeLoop(ctx context.Context, logger *zap.Logger, prober interface{ Probe(context.Context) }, interval time.Duration) {
	if prober == nil || interval <= 0 {
		return
	}

	if logger != nil {
		logger.Info("provider probe loop enabled", zap.Duration("interval", interval))
	}

	go func() {
		prober.Probe(ctx)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				prober.Probe(ctx)
			}
		}
	}()
}

func buildProviderBackends(cfg config.Config) ([]providerrouter.Backend, []modelsservice.Source, error) {
	if len(cfg.Provider.Backends) > 0 {
		backends, err := buildConfiguredProviderBackends(cfg.Provider.Backends, cfg.Gateway.DefaultModel)
		if err != nil {
			return nil, nil, err
		}
		return backends, collectModelSources(backends), nil
	}

	primary, err := buildProviderBackend("primary", cfg.Provider.Primary, cfg.Gateway.DefaultModel, providermock.Config{
		FailCreate: cfg.Gateway.PrimaryMockFailCreate,
		FailStream: cfg.Gateway.PrimaryMockFailStream,
	})
	if err != nil {
		return nil, nil, err
	}

	secondary, err := buildProviderBackend("secondary", cfg.Provider.Secondary, cfg.Gateway.DefaultModel, providermock.Config{})
	if err != nil {
		return nil, nil, err
	}

	backends := []providerrouter.Backend{primary, secondary}
	return backends, collectModelSources(backends), nil
}

func buildConfiguredProviderBackends(cfgs []config.ProviderEndpointConfig, defaultModel string) ([]providerrouter.Backend, error) {
	backends := make([]providerrouter.Backend, 0, len(cfgs))
	for index, providerCfg := range cfgs {
		backend, err := buildProviderBackend(fmt.Sprintf("backends[%d]", index), providerCfg, defaultModel, providermock.Config{})
		if err != nil {
			return nil, err
		}
		backends = append(backends, backend)
	}

	if len(backends) == 0 {
		return nil, errors.New("at least one provider backend is required")
	}

	return backends, nil
}

func collectModelSources(backends []providerrouter.Backend) []modelsservice.Source {
	sources := make([]modelsservice.Source, 0, len(backends))
	for _, backend := range backends {
		if source, ok := backend.Provider.(modelsservice.Source); ok {
			sources = append(sources, source)
		}
	}
	return sources
}

func applyRouteRules(backends []providerrouter.Backend, rules []mysqlstore.RouteRuleRecord) ([]providerrouter.Backend, bool, error) {
	if len(rules) == 0 {
		return backends, false, nil
	}

	byBackend := make(map[string][]providerrouter.RouteRule, len(rules))
	minPriority := make(map[string]int, len(rules))
	for _, rule := range rules {
		backendName := strings.TrimSpace(rule.BackendName)
		if backendName == "" {
			return nil, false, errors.New("route rule backend_name is required")
		}

		normalizedModel := strings.TrimSpace(strings.ToLower(rule.Model))
		byBackend[backendName] = append(byBackend[backendName], providerrouter.RouteRule{
			Model:    normalizedModel,
			Priority: rule.Priority,
		})
		if current, ok := minPriority[backendName]; !ok || rule.Priority < current {
			minPriority[backendName] = rule.Priority
		}
	}

	filtered := make([]providerrouter.Backend, 0, len(byBackend))
	seen := make(map[string]struct{}, len(byBackend))
	for _, backend := range backends {
		routedRules, ok := byBackend[backend.Name]
		if !ok {
			continue
		}

		backend.RouteRules = append([]providerrouter.RouteRule(nil), routedRules...)
		backend.Models = nil
		backend.Priority = minPriority[backend.Name]
		filtered = append(filtered, backend)
		seen[backend.Name] = struct{}{}
	}

	for backendName := range byBackend {
		if _, ok := seen[backendName]; ok {
			continue
		}
		return nil, false, fmt.Errorf("route rule references unknown backend %q", backendName)
	}

	if len(filtered) == 0 {
		return nil, false, errors.New("route rules configured but no routed backends remained")
	}

	return filtered, true, nil
}

func buildProviderBackend(role string, cfg config.ProviderEndpointConfig, defaultModel string, mockCfg providermock.Config) (providerrouter.Backend, error) {
	providerType := strings.ToLower(strings.TrimSpace(cfg.Type))
	if providerType == "" {
		providerType = "mock"
	}

	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = fmt.Sprintf("%s-%s", providerType, role)
	}

	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = defaultModel
	}
	models := normalizeModels(cfg.Models)

	switch providerType {
	case "mock":
		return providerrouter.Backend{
			Name:     name,
			Priority: cfg.Priority,
			Models:   models,
			Provider: providermock.NewWithConfig(providermock.Config{
				ResponseText: mockCfg.ResponseText,
				StreamParts:  mockCfg.StreamParts,
				FailCreate:   mockCfg.FailCreate,
				FailStream:   mockCfg.FailStream,
				Model:        model,
			}),
		}, nil
	case "openai":
		if strings.TrimSpace(cfg.BaseURL) == "" {
			return providerrouter.Backend{}, fmt.Errorf("%s provider base_url is required for type openai", role)
		}

		return providerrouter.Backend{
			Name:              name,
			Priority:          cfg.Priority,
			Models:            models,
			FirstEventTimeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
			Provider: provideropenai.New(provideropenai.Config{
				BaseURL:      cfg.BaseURL,
				APIKey:       cfg.APIKey,
				DefaultModel: model,
				Timeout:      time.Duration(cfg.TimeoutSeconds) * time.Second,
				MaxRetries:   cfg.MaxRetries,
				RetryBackoff: time.Duration(cfg.RetryBackoffMilliseconds) * time.Millisecond,
			}),
		}, nil
	case "anthropic":
		if strings.TrimSpace(cfg.BaseURL) == "" {
			return providerrouter.Backend{}, fmt.Errorf("%s provider base_url is required for type anthropic", role)
		}

		return providerrouter.Backend{
			Name:              name,
			Priority:          cfg.Priority,
			Models:            models,
			FirstEventTimeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
			Provider: provideranthropic.New(provideranthropic.Config{
				BaseURL:      cfg.BaseURL,
				APIKey:       cfg.APIKey,
				DefaultModel: model,
				MaxTokens:    cfg.MaxTokens,
				Timeout:      time.Duration(cfg.TimeoutSeconds) * time.Second,
				MaxRetries:   cfg.MaxRetries,
				RetryBackoff: time.Duration(cfg.RetryBackoffMilliseconds) * time.Millisecond,
			}),
		}, nil
	case "ollama":
		if strings.TrimSpace(cfg.BaseURL) == "" {
			return providerrouter.Backend{}, fmt.Errorf("%s provider base_url is required for type ollama", role)
		}

		return providerrouter.Backend{
			Name:              name,
			Priority:          cfg.Priority,
			Models:            models,
			FirstEventTimeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
			Provider: providerollama.New(providerollama.Config{
				BaseURL:      cfg.BaseURL,
				APIKey:       cfg.APIKey,
				DefaultModel: model,
				Timeout:      time.Duration(cfg.TimeoutSeconds) * time.Second,
				MaxRetries:   cfg.MaxRetries,
				RetryBackoff: time.Duration(cfg.RetryBackoffMilliseconds) * time.Millisecond,
			}),
		}, nil
	default:
		return providerrouter.Backend{}, fmt.Errorf("%s provider type %q is not supported", role, cfg.Type)
	}
}

func normalizeModels(models []string) []string {
	if len(models) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		trimmed := strings.TrimSpace(strings.ToLower(model))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

type providerHealthAdapter struct {
	provider interface {
		Ready() bool
		BackendStatuses() []providerrouter.BackendStatus
	}
}

type providerEventLogger struct {
	logger *zap.Logger
}

type multiProviderObserver struct {
	observers []providerrouter.Observer
}

func (o multiProviderObserver) OnEvent(event providerrouter.Event) {
	for _, observer := range o.observers {
		if observer == nil {
			continue
		}
		observer.OnEvent(event)
	}
}

func (l providerEventLogger) OnEvent(event providerrouter.Event) {
	if l.logger == nil {
		return
	}

	fields := []zap.Field{
		zap.String("type", event.Type),
		zap.String("operation", event.Operation),
		zap.String("backend", event.Backend),
		zap.Int("attempt", event.Attempt),
		zap.Int("consecutive_failures", event.ConsecutiveFailures),
	}
	if event.Duration > 0 {
		fields = append(fields, zap.Duration("duration", event.Duration))
	}
	if !event.UnhealthyUntil.IsZero() {
		fields = append(fields, zap.String("unhealthy_until", event.UnhealthyUntil.Format(time.RFC3339)))
	}
	if event.Error != "" {
		fields = append(fields, zap.String("reason", event.Error))
	}

	l.logger.Info("provider event", fields...)
}

func (a providerHealthAdapter) Ready() bool {
	if a.provider == nil {
		return true
	}
	return a.provider.Ready()
}

func (a providerHealthAdapter) BackendStatuses() []handlers.ProviderBackendStatus {
	if a.provider == nil {
		return nil
	}

	statuses := a.provider.BackendStatuses()
	result := make([]handlers.ProviderBackendStatus, 0, len(statuses))
	for _, status := range statuses {
		result = append(result, handlers.ProviderBackendStatus{
			Name:                status.Name,
			Priority:            status.Priority,
			Models:              append([]string(nil), status.Models...),
			RouteRules:          toHandlerRouteRules(status.RouteRules),
			Healthy:             status.Healthy,
			ConsecutiveFailures: status.ConsecutiveFailures,
			UnhealthyUntil:      status.UnhealthyUntil,
			LastProbeAt:         status.LastProbeAt,
			LastProbeError:      status.LastProbeError,
		})
	}
	return result
}

func toHandlerRouteRules(rules []providerrouter.RouteRule) []handlers.RouteRule {
	if len(rules) == 0 {
		return nil
	}

	result := make([]handlers.RouteRule, 0, len(rules))
	for _, rule := range rules {
		result = append(result, handlers.RouteRule{
			Model:    rule.Model,
			Priority: rule.Priority,
		})
	}
	return result
}
