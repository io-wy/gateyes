package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/handler"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gateyes/gateway/internal/service/router"
	"github.com/gateyes/gateway/internal/service/streaming"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Warn("failed to load config, using defaults", "error", err)
		cfg = config.DefaultConfig()
	}

	// 初始化各层组件
	apiKeyRepo := repository.NewAPIKeyRepository(cfg.APIKeys)
	userRepo := repository.NewUserRepository()
	metrics := handler.NewMetrics(cfg.Metrics.Namespace)

	// Provider 层
	providerMgr := provider.NewManager(cfg.Providers)

	// Cache 层 (KV Cache)
	kvCache := cache.NewMemoryCache(cfg.Cache)

	// Limiter 层
	limiterSvc := limiter.NewLimiter(cfg.Limiter)

	// Router 层
	routerSvc := router.NewRouter(cfg.Router)
	routerSvc.SetProviders(providerMgr.List())

	// Streaming 服务
	streamingSvc := streaming.NewStreaming()

	// 初始化 Handler
	h := handler.NewHandler(&handler.Dependencies{
		Config:      cfg,
		APIKeyRepo:  apiKeyRepo,
		UserRepo:    userRepo,
		Metrics:     metrics,
		ProviderMgr: providerMgr,
		KVCache:     kvCache,
		Limiter:     limiterSvc,
		Router:      routerSvc,
		Streaming:   streamingSvc,
	})

	// 初始化 Admin Handler
	adminHandler := handler.NewAdminHandler(userRepo, providerMgr, cfg.Admin.AdminKey)

	// 启动服务器
	srv := handler.NewServer(cfg.Server, h, adminHandler)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("gateway listening", "addr", cfg.Server.ListenAddr)
		if err := srv.Start(); err != nil {
			if err == handler.ErrServerClosed {
				return
			}
			slog.Error("server stopped with error", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}
