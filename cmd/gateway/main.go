package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/domain/plugin"
	"github.com/gateyes/gateway/internal/handler"
	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/pkg/eventbus"
	"github.com/gateyes/gateway/internal/pkg/logging"
	redispkg "github.com/gateyes/gateway/internal/pkg/redis"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/db"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/alert"
	batchSvc "github.com/gateyes/gateway/internal/service/batch"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/catalog"
	grpcplugin "github.com/gateyes/gateway/internal/service/extension/plugin/grpc"
	wasmplugin "github.com/gateyes/gateway/internal/service/extension/plugin/wasm"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/pricing"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
	"github.com/gateyes/gateway/internal/service/router"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// compositePluginManager combines gRPC and WASM plugin managers.
type compositePluginManager struct {
	grpcMgr *grpcplugin.Manager
	wasmMgr *wasmplugin.Manager
}

func (c *compositePluginManager) Router() plugin.Router {
	if c.grpcMgr == nil {
		return nil
	}
	return c.grpcMgr.Router()
}

func (c *compositePluginManager) GetByPhase(phase plugin.Phase) []plugin.Gateway {
	var result []plugin.Gateway
	if c.grpcMgr != nil {
		result = append(result, c.grpcMgr.GetByPhase(phase)...)
	}
	if c.wasmMgr != nil {
		result = append(result, c.wasmMgr.GetByPhase(phase)...)
	}
	return result
}

func (c *compositePluginManager) Close() error {
	var firstErr error
	if c.grpcMgr != nil {
		if err := c.grpcMgr.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if c.wasmMgr != nil {
		if err := c.wasmMgr.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

var _ plugin.Manager = (*compositePluginManager)(nil)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	slog.SetDefault(slog.New(logging.NewTraceHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))))

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Warn("failed to load config, using defaults", "error", err)
		cfg = config.DefaultConfig()
	}

	shutdownTracer := initTracer(cfg.Tracing)
	defer shutdownTracer()

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

	if err := seedTenantProviders(context.Background(), store, defaultTenant.ID, enabledProviderNames(cfg.Providers)); err != nil {
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

	metrics := handler.NewMetricsFromConfig(cfg.Metrics)
	providerMgr, err := provider.NewManager(cfg.Providers)
	if err != nil {
		slog.Error("failed to initialize providers", "error", err)
		os.Exit(1)
	}
	if err := seedProviderRegistry(context.Background(), store, cfg.Providers); err != nil {
		slog.Error("failed to seed provider registry", "error", err)
		os.Exit(1)
	}
	if records, err := store.ListProviderRegistry(context.Background()); err != nil {
		slog.Error("failed to load provider registry", "error", err)
		os.Exit(1)
	} else {
		for _, record := range records {
			if err := providerMgr.UpsertRuntimeProvider(record); err != nil {
				slog.Error("failed to hydrate runtime provider", "provider", record.Name, "error", err)
				os.Exit(1)
			}
		}
		providerMgr.ApplyRegistry(records)
	}

	limiterSvc := limiter.NewLimiter(cfg.Limiter)
	routerSvc := router.NewRouter(cfg.Router, providerMgr.Stats)

	if cfg.Router.InferenceMetrics.Enabled {
		endpoints := make(map[string]string, len(cfg.Providers))
		for _, p := range cfg.Providers {
			if p.Enabled && p.MetricsURL != "" {
				endpoints[p.Name] = p.MetricsURL
			}
		}
		if len(endpoints) > 0 {
			interval := time.Duration(cfg.Router.InferenceMetrics.ScrapeIntervalSeconds) * time.Second
			scraper := router.NewInferenceScraper(endpoints, interval)
			routerSvc.SetInferenceScraper(scraper)
			metrics.SetInferenceScraper(scraper)
			defer scraper.Stop()
			// Started below once we have the signal-aware ctx.
			go scraper.Start(context.Background())
		}
	}

	// Redis for distributed rate limiting and alert dedup
	var redisClient *redis.Client
	if cfg.Redis.Enabled() {
		var err error
		redisClient, err = redispkg.NewClient(redisClientConfig(cfg.Redis))
		if err != nil {
			slog.Error("failed to connect to Redis", "error", err)
			os.Exit(1)
		}
		defer redispkg.Close(redisClient)
		limiterSvc.SetRedis(redisClient)
		slog.Info("Redis connected", "addr", cfg.Redis.Addr)
	}
	routerSvc.SetProviders(providerMgr.List())

	// 初始化配额预警服务
	alertSvc := alert.NewAlertService(cfg.Alert, store)
	if redisClient != nil {
		alertSvc.SetRedis(redisClient)
	}
	healthChecker := provider.NewHealthChecker(cfg.HealthCheck, store, providerMgr, alertSvc)
	budgetSvc := budget.New(store)

	reloader := config.NewReloader(*configPath)
	reloader.Register(limiterSvc, routerSvc, alertSvc, providerMgr)

	// Cache: Redis primary + Memory fallback
	var cacheSvc cache.Cache
	if cfg.Cache.Enabled {
		memCache := cache.NewMemoryCache(cache.MemoryConfig{
			Capacity:   cfg.Cache.Capacity,
			DefaultTTL: time.Duration(cfg.Cache.DefaultTTL) * time.Second,
		})
		if cfg.Redis.Enabled() && cfg.Cache.Backend != "memory" {
			redisCache := cache.NewRedisCache(redisClient, cache.RedisConfig{
				DefaultTTL: time.Duration(cfg.Cache.DefaultTTL) * time.Second,
			})
			cacheSvc = cache.NewFallbackCache(redisCache, memCache)
		} else {
			cacheSvc = memCache
		}
	}

	httpMiddleware := middleware.New(cfg, store, limiterSvc, budgetSvc, alertSvc, metrics, redisClient)

	persistBus := eventbus.New(eventbus.Options{
		Buffer:         cfg.Persistence.BusBuffer,
		Workers:        cfg.Persistence.BusWorkers,
		HandlerTimeout: time.Duration(cfg.Persistence.HandlerTimeoutSeconds) * time.Second,
		Metrics:        metrics,
		Kafka: eventbus.KafkaOptions{
			Enabled:       cfg.Persistence.Kafka.Enabled,
			Brokers:       cfg.Persistence.Kafka.Brokers,
			Topic:         cfg.Persistence.Kafka.Topic,
			ConsumerGroup: cfg.Persistence.Kafka.ConsumerGroup,
			ClientID:      cfg.Persistence.Kafka.ClientID,
			BatchSize:     cfg.Persistence.Kafka.BatchSize,
			BatchTimeout:  time.Duration(cfg.Persistence.Kafka.BatchTimeoutMs) * time.Millisecond,
			ReadMinBytes:  cfg.Persistence.Kafka.ReadMinBytes,
			ReadMaxBytes:  cfg.Persistence.Kafka.ReadMaxBytes,
			MaxAttempts:   cfg.Persistence.Kafka.MaxAttempts,
		},
	})
	store.SetEventBus(persistBus)

	guardrails := buildGuardrails(cfg.Guardrails)

	var pricingFeed *pricing.Feed
	if cfg.Pricing.Enabled {
		interval := time.Duration(cfg.Pricing.RefreshIntervalSeconds) * time.Second
		pricingFeed = pricing.New(pricing.Options{
			URL:       cfg.Pricing.FeedURL,
			CacheFile: cfg.Pricing.CacheFile,
			Interval:  interval,
		})
		if err := pricingFeed.Bootstrap(); err != nil {
			slog.Warn("pricing feed bootstrap failed", "error", err)
		}
		pricingFeed.Start(context.Background())
		defer pricingFeed.Stop()
	}

	// Hydrate marketplace plugins from DB on top of static config.
	cfg.GRPCPlugins, cfg.WASMPlugins = loadMarketplacePlugins(context.Background(), store, defaultTenant.ID, cfg.GRPCPlugins, cfg.WASMPlugins)

	// gRPC plugin manager
	var grpcMgr *grpcplugin.Manager
	if len(cfg.GRPCPlugins) > 0 {
		pm, err := grpcplugin.NewManager(cfg.GRPCPlugins)
		if err != nil {
			slog.Warn("failed to initialize some grpc plugins", "error", err)
		}
		grpcMgr = pm
	}

	// WASM plugin manager
	var wasmMgr *wasmplugin.Manager
	if len(cfg.WASMPlugins) > 0 {
		wm, err := wasmplugin.NewManager(cfg.WASMPlugins)
		if err != nil {
			slog.Warn("failed to initialize some wasm plugins", "error", err)
		}
		wasmMgr = wm
	}

	// Unified plugin manager (gRPC + WASM)
	var pluginManager plugin.Manager
	if grpcMgr != nil || wasmMgr != nil {
		pluginManager = &compositePluginManager{grpcMgr: grpcMgr, wasmMgr: wasmMgr}
	}

	responsesService := responseSvc.New(&responseSvc.Dependencies{
		Config:        cfg,
		Store:         store,
		Auth:          httpMiddleware.AuthService(),
		ProviderMgr:   providerMgr,
		Router:        routerSvc,
		Alert:         alertSvc,
		Limiter:       limiterSvc,
		Cache:         cacheSvc,
		Metrics:       metrics,
		EventBus:      persistBus,
		Guardrails:    guardrails,
		PricingFeed:   pricingFeed,
		PluginManager: pluginManager,
	})
	if redisClient != nil {
		responsesService.SetRedis(redisClient)
		responsesService.RestoreCircuitBreakerState(context.Background())
	}
	batchService := batchSvc.New(batchSvc.Dependencies{
		Store:     store,
		Responses: responsesService,
		EventBus:  persistBus,
	})
	catalogSvc := catalog.New(&catalog.Dependencies{
		Store:     store,
		Auth:      httpMiddleware.AuthService(),
		Limiter:   limiterSvc,
		BudgetSvc: budgetSvc,
		AlertSvc:  alertSvc,
		Responses: responsesService,
	})

	redisPing := func(ctx context.Context) error {
		if redisClient == nil {
			return nil
		}
		return redisClient.Ping(ctx).Err()
	}

	h := handler.NewHandler(&handler.Dependencies{
		Config:      cfg,
		Store:       store,
		Metrics:     metrics,
		ProviderMgr: providerMgr,
		ResponseSvc: responsesService,
		CatalogSvc:  catalogSvc,
		BatchSvc:    batchService,
		RedisPing:   redisPing,
	})

	adminHandler := handler.NewAdminHandler(store, providerMgr, catalogSvc, reloader)
	adminHandler.SetRouter(routerSvc)
	adminHandler.SetMetrics(metrics)
	adminHandler.SetHealthChecker(healthChecker)
	adminHandler.SetPluginDirectory(cfg.Plugins.Directory)

	// Start the event bus after all handlers have registered.
	persistBus.Start(context.Background())

	srv := handler.NewServer(cfg.Server, h, adminHandler, httpMiddleware)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go metrics.StartProviderStatsExporter(ctx, providerMgr.Stats, 5*time.Second)

	go func() {
		slog.Info("pprof listening", "addr", ":6060")
		if err := http.ListenAndServe(":6060", nil); err != nil {
			slog.Error("pprof server stopped", "error", err)
		}
	}()

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

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				h.SyncCircuitBreakerStates()
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		// Run once on startup as well
		slog.Info("starting response TTL cleanup (30-day retention)")
		if deleted, err := store.DeleteResponsesOlderThan(ctx, time.Now().UTC().AddDate(0, 0, -30)); err != nil {
			slog.Error("response TTL cleanup failed", "error", err)
		} else if deleted > 0 {
			slog.Info("response TTL cleanup completed", "deleted", deleted)
		}
		for {
			select {
			case <-ticker.C:
				slog.Info("running response TTL cleanup (30-day retention)")
				if deleted, err := store.DeleteResponsesOlderThan(ctx, time.Now().UTC().AddDate(0, 0, -30)); err != nil {
					slog.Error("response TTL cleanup failed", "error", err)
				} else if deleted > 0 {
					slog.Info("response TTL cleanup completed", "deleted", deleted)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	healthChecker.Start(ctx)

	<-ctx.Done()
	shutdownTimeout := time.Duration(cfg.Server.ShutdownTimeout) * time.Second
	if shutdownTimeout <= 0 {
		shutdownTimeout = 10 * time.Second
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
	limiterSvc.Stop()
	if err := persistBus.Close(); err != nil {
		slog.Warn("persistence event bus drain timeout", "error", err)
	}
	if dropped := persistBus.Dropped(); dropped > 0 {
		slog.Warn("persistence event bus dropped events during run", "count", dropped)
	}
	providerMgr.CloseIdleConnections()
	if pluginManager != nil {
		if err := pluginManager.Close(); err != nil {
			slog.Warn("failed to close grpc plugin manager", "error", err)
		}
	}
	if guardrails != nil {
		if err := guardrails.Close(); err != nil {
			slog.Warn("failed to close guardrails", "error", err)
		}
	}
	if pm := httpMiddleware.PluginManager(); pm != nil {
		pm.Close()
	}
}

func loadMarketplacePlugins(
	ctx context.Context,
	store repository.Store,
	tenantID string,
	grpcCfgs []config.GRPCPluginConfig,
	wasmCfgs []config.WASMPluginConfig,
) ([]config.GRPCPluginConfig, []config.WASMPluginConfig) {
	if tenantID == "" {
		return grpcCfgs, wasmCfgs
	}

	plugins, err := store.ListPlugins(ctx, tenantID, repository.PluginFilter{Enabled: boolPtr(true)})
	if err != nil {
		slog.Warn("failed to load marketplace plugins", "error", err)
		return grpcCfgs, wasmCfgs
	}

	for _, p := range plugins {
		switch p.Type {
		case "grpc":
			if p.Address == "" {
				continue
			}
			grpcCfgs = append(grpcCfgs, config.GRPCPluginConfig{
				Name:    p.Name,
				Type:    "gateway",
				Address: p.Address,
				Timeout: p.TimeoutMs,
				Phases:  p.Phases,
			})
		case "wasm":
			if p.FilePath == "" {
				continue
			}
			wasmCfgs = append(wasmCfgs, config.WASMPluginConfig{
				Name:        p.Name,
				Path:        p.FilePath,
				Phases:      p.Phases,
				TimeoutMs:   p.TimeoutMs,
				MemoryPages: uint32(p.MemoryPages),
			})
		}
	}
	return grpcCfgs, wasmCfgs
}

func boolPtr(v bool) *bool {
	return &v
}

func redisClientConfig(cfg config.RedisConfig) redispkg.Config {
	return redispkg.Config{
		Addr:           cfg.Addr,
		Password:       cfg.Password,
		DB:             cfg.DB,
		MinIdleConns:   cfg.MinIdleConns,
		MaxRetries:     cfg.MaxRetries,
		PoolSize:       cfg.PoolSize,
		DialTimeoutMs:  cfg.DialTimeoutMs,
		ReadTimeoutMs:  cfg.ReadTimeoutMs,
		WriteTimeoutMs: cfg.WriteTimeoutMs,
	}
}

func initTracer(cfg config.TracingConfig) func() {
	if !cfg.Enabled {
		return func() {}
	}

	var exporter sdktrace.SpanExporter
	var err error

	switch cfg.Exporter {
	case "otlp":
		opts := []otlptracehttp.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpointURL(cfg.Endpoint))
		}
		exporter, err = otlptracehttp.New(context.Background(), opts...)
		if err != nil {
			slog.Warn("failed to create OTLP trace exporter", "error", err)
			return func() {}
		}
	default:
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			slog.Warn("failed to create stdout trace exporter", "error", err)
			return func() {}
		}
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("gateyes-gateway"),
			semconv.ServiceVersion("1.0.0"),
		)),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := provider.Shutdown(ctx); err != nil {
			slog.Warn("failed to shutdown tracer provider", "error", err)
		}
	}
}
