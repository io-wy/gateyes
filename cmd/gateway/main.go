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
	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/handler"
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
	"github.com/gateyes/gateway/internal/service/router"
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

	database, err := db.Open(cfg.Database)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	if cfg.Database.AutoMigrate {
		if err := database.Migrate(context.Background()); err != nil {
			slog.Error("failed to migrate database", "error", err)
			os.Exit(1)
		}
	}

	store := sqlstore.New(database)
	defaultTenant, err := store.EnsureTenant(context.Background(), repository.EnsureTenantParams{
		ID:     cfg.Admin.DefaultTenant,
		Slug:   cfg.Admin.DefaultTenant,
		Name:   cfg.Admin.DefaultTenant,
		Status: repository.StatusActive,
	})
	if err != nil {
		slog.Error("failed to ensure default tenant", "error", err)
		os.Exit(1)
	}

	if err := store.ReplaceTenantProviders(context.Background(), defaultTenant.ID, enabledProviderNames(cfg.Providers)); err != nil {
		slog.Error("failed to seed default tenant providers", "error", err)
		os.Exit(1)
	}
	if err := store.BackfillDefaultTenant(context.Background(), defaultTenant.ID); err != nil {
		slog.Error("failed to backfill default tenant", "error", err)
		os.Exit(1)
	}

	if err := seedConfiguredAPIKeys(context.Background(), store, defaultTenant.ID, cfg.APIKeys); err != nil {
		slog.Error("failed to seed configured api keys", "error", err)
		os.Exit(1)
	}
	if err := seedBootstrapAdmin(context.Background(), store, defaultTenant.ID, cfg.Admin); err != nil {
		slog.Error("failed to seed bootstrap admin", "error", err)
		os.Exit(1)
	}

	metrics := handler.NewMetrics(cfg.Metrics.Namespace)
	providerMgr, err := provider.NewManager(cfg.Providers)
	if err != nil {
		slog.Error("failed to initialize providers", "error", err)
		os.Exit(1)
	}

	kvCache := cache.NewMemoryCache(cfg.Cache)
	limiterSvc := limiter.NewLimiter(cfg.Limiter)
	routerSvc := router.NewRouter(cfg.Router)
	routerSvc.SetProviders(providerMgr.List())
	httpMiddleware := middleware.New(store, limiterSvc)
	responsesService := responseSvc.New(&responseSvc.Dependencies{
		Config:      cfg,
		Store:       store,
		Auth:        httpMiddleware.AuthService(),
		ProviderMgr: providerMgr,
		Router:      routerSvc,
		Cache:       kvCache,
	})

	h := handler.NewHandler(&handler.Dependencies{
		Config:      cfg,
		Store:       store,
		Metrics:     metrics,
		ProviderMgr: providerMgr,
		ResponseSvc: responsesService,
	})

	adminHandler := handler.NewAdminHandler(store, providerMgr)
	srv := handler.NewServer(cfg.Server, h, adminHandler, httpMiddleware)

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

func seedConfiguredAPIKeys(ctx context.Context, store repository.IdentityStore, tenantID string, configured []config.APIKeyConfig) error {
	for _, item := range configured {
		if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
			TenantID:   tenantID,
			Key:        item.Key,
			SecretHash: repository.HashSecret(item.Secret),
			Name:       "bootstrap-" + item.Key,
			Role:       repository.RoleTenantUser,
			Quota:      item.Quota,
			QPS:        item.QPS,
			Models:     item.Models,
		}); err != nil {
			return err
		}
	}
	return nil
}

func seedBootstrapAdmin(ctx context.Context, store repository.IdentityStore, tenantID string, cfg config.AdminConfig) error {
	if cfg.BootstrapKey == "" || cfg.BootstrapSecret == "" {
		return nil
	}

	return store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenantID,
		Key:        cfg.BootstrapKey,
		SecretHash: repository.HashSecret(cfg.BootstrapSecret),
		Name:       "bootstrap-admin",
		Role:       repository.RoleSuperAdmin,
		Quota:      -1,
		QPS:        0,
		Models:     nil,
	})
}

func enabledProviderNames(providers []config.ProviderConfig) []string {
	names := make([]string, 0, len(providers))
	for _, provider := range providers {
		if provider.Enabled {
			names = append(names, provider.Name)
		}
	}
	return names
}
