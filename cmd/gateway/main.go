package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MoChengqian/llm-access-gateway/internal/config"
	httpserver "github.com/MoChengqian/llm-access-gateway/internal/http"
	"github.com/MoChengqian/llm-access-gateway/internal/service/chat"
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

	chatService := chat.NewMockService(cfg.Gateway.DefaultModel)

	server := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           httpserver.NewRouter(logger, chatService),
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
