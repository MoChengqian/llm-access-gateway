package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/api"
	"github.com/MoChengqian/llm-access-gateway/internal/api/handlers"
	"github.com/MoChengqian/llm-access-gateway/internal/auth"
	"github.com/MoChengqian/llm-access-gateway/internal/config"
	"github.com/MoChengqian/llm-access-gateway/internal/obs/metrics"
	providermock "github.com/MoChengqian/llm-access-gateway/internal/provider/mock"
	providerrouter "github.com/MoChengqian/llm-access-gateway/internal/provider/router"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
	"github.com/MoChengqian/llm-access-gateway/internal/service/governance"
	mysqlstore "github.com/MoChengqian/llm-access-gateway/internal/store/mysql"
	redisstore "github.com/MoChengqian/llm-access-gateway/internal/store/redis"
	_ "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	loggerConfig := zap.NewProductionConfig()
	loggerConfig.Level = cfg.Log.Level

	logger, err := loggerConfig.Build()
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = logger.Sync()
	}()

	if cfg.MySQL.DSN == "" {
		logger.Fatal("mysql dsn is required", zap.String("field", "mysql.dsn"))
	}

	db, err := sql.Open("mysql", cfg.MySQL.DSN)
	if err != nil {
		logger.Fatal("mysql open failed", zap.Error(err))
	}
	defer func() {
		_ = db.Close()
	}()

	if err := db.PingContext(context.Background()); err != nil {
		logger.Fatal("mysql ping failed", zap.Error(err))
	}

	authStore := mysqlstore.NewAuthStore(db)
	authService := auth.NewService(authStore)
	governanceStore := mysqlstore.NewGovernanceStore(db)
	limiter := governance.Limiter(governance.NewMySQLLimiter(governanceStore))
	if cfg.Redis.Address != "" {
		redisClient := redisstore.NewClient(redisstore.Config{
			Address:  cfg.Redis.Address,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		if err := redisClient.Ping(context.Background()); err != nil {
			logger.Error("redis ping failed, falling back to mysql limiter", zap.Error(err))
		} else {
			limiter = governance.NewRedisLimiter(redisClient, limiter)
			logger.Info("redis limiter enabled", zap.String("address", cfg.Redis.Address), zap.Int("db", cfg.Redis.DB))
		}
	}
	governanceService := governance.NewService(governanceStore, limiter)
	metricsRegistry := metrics.NewRegistry()
	chatProvider := providerrouter.New([]providerrouter.Backend{
		{
			Name: "mock-primary",
			Provider: providermock.NewWithConfig(providermock.Config{
				FailCreate: cfg.Gateway.PrimaryMockFailCreate,
				FailStream: cfg.Gateway.PrimaryMockFailStream,
			}),
		},
		{
			Name:     "mock-secondary",
			Provider: providermock.New(),
		},
	}, providerrouter.Config{
		FailureThreshold: cfg.Gateway.ProviderFailureThreshold,
		Cooldown:         time.Duration(cfg.Gateway.ProviderCooldownSeconds) * time.Second,
		Observer: multiProviderObserver{
			observers: []providerrouter.Observer{
				providerEventLogger{logger: logger},
				metricsRegistry,
			},
		},
	})
	chatService := chat.NewService(cfg.Gateway.DefaultModel, chatProvider)

	server := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           api.NewRouter(logger, chatService, authService, governanceService, providerHealthAdapter{provider: chatProvider}, metricsRegistry, metricsRegistry),
		ReadHeaderTimeout: 5 * time.Second,
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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", zap.Error(err))
	}
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
			Healthy:             status.Healthy,
			ConsecutiveFailures: status.ConsecutiveFailures,
			UnhealthyUntil:      status.UnhealthyUntil,
		})
	}
	return result
}
