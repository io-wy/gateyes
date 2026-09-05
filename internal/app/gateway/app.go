package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/application/administration"
	"github.com/gateyes/gateway/internal/application/inference"
	"github.com/gateyes/gateway/internal/handler"
	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/pkg/eventbus"
	"github.com/gateyes/gateway/internal/pkg/logging"
	redispkg "github.com/gateyes/gateway/internal/pkg/redis"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/db"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/adminconsole"
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
)

func Run(ctx context.Context, configPath string) error {
	slog.SetDefault(slog.New(logging.NewTraceHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))))

	cfg, err := config.Load(configPath)
	if err != nil {
		slog.Warn("failed to load config, using defaults", "error", err)
		cfg = config.DefaultConfig()
	}

	shutdownTracer := InitTracer(cfg.Tracing)
	defer shutdownTracer()

	database, err := db.Open(cfg.Database)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	if cfg.Database.AutoMigrate {
		if err := database.Migrate(ctx); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	}

	store := sqlstore.New(database)
	defaultTenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     cfg.Admin.DefaultTenant,
		Slug:   cfg.Admin.DefaultTenant,
		Name:   cfg.Admin.DefaultTenant,
		Status: repository.StatusActive,
	})
	if err != nil {
		return fmt.Errorf("ensure default tenant: %w", err)
	}

	if err := SeedTenantProviders(ctx, store, defaultTenant.ID, EnabledProviderNames(cfg.Providers)); err != nil {
		return fmt.Errorf("seed default tenant providers: %w", err)
	}
	if err := store.BackfillDefaultTenant(ctx, defaultTenant.ID); err != nil {
		return fmt.Errorf("backfill default tenant: %w", err)
	}
	if err := SeedConfiguredAPIKeys(ctx, store, defaultTenant.ID, cfg.APIKeys); err != nil {
		return fmt.Errorf("seed configured api keys: %w", err)
	}
	if err := SeedBootstrapAdmin(ctx, store, defaultTenant.ID, cfg.Admin); err != nil {
		return fmt.Errorf("seed bootstrap admin: %w", err)
	}

	metrics := handler.NewMetricsFromConfig(cfg.Metrics)
	providerMgr, err := provider.NewManager(cfg.Providers)
	if err != nil {
		return fmt.Errorf("initialize providers: %w", err)
	}
	if err := SeedProviderRegistry(ctx, store, cfg.Providers); err != nil {
		return fmt.Errorf("seed provider registry: %w", err)
	}
	records, err := store.ListProviderRegistry(ctx)
	if err != nil {
		return fmt.Errorf("load provider registry: %w", err)
	}
	for _, record := range records {
		if err := providerMgr.UpsertRuntimeProvider(record); err != nil {
			return fmt.Errorf("hydrate runtime provider %s: %w", record.Name, err)
		}
	}
	providerMgr.ApplyRegistry(records)

	limiterSvc := limiter.NewLimiter(cfg.Limiter)
	routerSvc := router.NewRouter(cfg.Router, providerMgr.Stats)
	var inferenceScraper *router.InferenceScraper
	if cfg.Router.InferenceMetrics.Enabled {
		endpoints := make(map[string]string, len(cfg.Providers))
		for _, p := range cfg.Providers {
			if p.Enabled && p.MetricsURL != "" {
				endpoints[p.Name] = p.MetricsURL
			}
		}
		if len(endpoints) > 0 {
			interval := time.Duration(cfg.Router.InferenceMetrics.ScrapeIntervalSeconds) * time.Second
			inferenceScraper = router.NewInferenceScraper(endpoints, interval)
			routerSvc.SetInferenceScraper(inferenceScraper)
			metrics.SetInferenceScraper(inferenceScraper)
			defer inferenceScraper.Stop()
		}
	}

	var redisClient *redis.Client
	if cfg.Redis.Enabled() {
		redisClient, err = redispkg.NewClient(RedisClientConfig(cfg.Redis))
		if err != nil {
			return fmt.Errorf("connect redis: %w", err)
		}
		defer redispkg.Close(redisClient)
		store.SetRedis(redisClient)
		limiterSvc.SetRedis(redisClient)
		slog.Info("Redis connected", "addr", cfg.Redis.Addr)
	}
	routerSvc.SetProviders(providerMgr.List())

	alertSvc := alert.NewAlertService(cfg.Alert, store)
	if redisClient != nil {
		alertSvc.SetRedis(redisClient)
	}
	healthChecker := provider.NewHealthChecker(cfg.HealthCheck, store, providerMgr, alertSvc)
	budgetSvc := budget.New(store)

	reloader := config.NewReloader(configPath)
	reloader.Register(limiterSvc, routerSvc, alertSvc, providerMgr)

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
			cacheSvc = cache.NewLayeredCache(memCache, redisCache)
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

	guardrails := BuildGuardrails(cfg.Guardrails)
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

	configuredGRPCPlugins := append([]config.GRPCPluginConfig(nil), cfg.GRPCPlugins...)
	configuredWASMPlugins := append([]config.WASMPluginConfig(nil), cfg.WASMPlugins...)
	cfg.GRPCPlugins, cfg.WASMPlugins = HydrateMarketplacePlugins(ctx, store, defaultTenant.ID, cfg.GRPCPlugins, cfg.WASMPlugins)
	var grpcMgr *grpcplugin.Manager
	if len(cfg.GRPCPlugins) > 0 {
		pm, err := grpcplugin.NewManager(cfg.GRPCPlugins)
		if err != nil {
			slog.Warn("failed to initialize some grpc plugins", "error", err)
		}
		grpcMgr = pm
	}
	var wasmMgr *wasmplugin.Manager
	if len(cfg.WASMPlugins) > 0 {
		wm, err := wasmplugin.NewManager(cfg.WASMPlugins)
		if err != nil {
			slog.Warn("failed to initialize some wasm plugins", "error", err)
		}
		wasmMgr = wm
	}
	pluginManager := NewCompositePluginManager(grpcMgr, wasmMgr)

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
		responsesService.RestoreCircuitBreakerState(ctx)
	}
	inferenceService := inference.NewOrchestrated(inference.Dependencies{
		Executor: responsesService,
	})
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
	providerRuntimeSvc := provider.NewRuntimeRegistryService(store, providerMgr)
	consoleSvc := administration.NewConsole(adminconsole.New(store, catalogSvc, providerRuntimeSvc))
	catalogApp := administration.NewCatalog(catalogSvc)
	runtimeConfig := administration.NewRuntimeConfig(reloader)

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
		ResponseSvc: inferenceService,
		CatalogSvc:  catalogSvc,
		BatchSvc:    batchService,
		RedisPing:   redisPing,
	})

	adminHandler := handler.NewAdminHandler(handler.AdminDependencies{
		Store:           store,
		ProviderManager: providerMgr,
		ProviderRuntime: providerRuntimeSvc,
		Console:         consoleSvc,
		Catalog:         catalogApp,
		RuntimeConfig:   runtimeConfig,
	})
	adminHandler.SetAuthService(httpMiddleware.AuthService())
	adminHandler.SetRouter(routerSvc)
	adminHandler.SetMetrics(metrics)
	adminHandler.SetHealthChecker(healthChecker)
	adminHandler.SetPluginDirectory(cfg.Plugins.Directory)
	adminHandler.SetConfiguredPlugins(configuredGRPCPlugins, configuredWASMPlugins)

	persistBus.Start(ctx)
	srv := handler.NewServer(cfg.Server, h, adminHandler, httpMiddleware)
	runtime := NewRuntime(srv, Options{
		ShutdownTimeout:   time.Duration(cfg.Server.ShutdownTimeout) * time.Second,
		PprofAddr:         ":6060",
		ServerClosedError: handler.ErrServerClosed,
	})
	if inferenceScraper != nil {
		runtime.Go("inference-scraper", func(ctx context.Context) {
			inferenceScraper.Start(ctx)
		})
	}
	runtime.Go("provider-stats-exporter", func(ctx context.Context) {
		metrics.StartProviderStatsExporter(ctx, providerMgr.Stats, 5*time.Second)
	})
	runtime.Go("batch-recovery", func(ctx context.Context) {
		batchService.StartRecovery(ctx, 30*time.Second, 2*time.Minute, 1000)
	})
	runtime.Go("circuit-breaker-sync", func(ctx context.Context) {
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
	})
	runtime.Go("response-ttl-cleanup", func(ctx context.Context) {
		runResponseTTLCleanup(ctx, store)
	})
	runtime.Go("budget-ledger-flush", func(ctx context.Context) {
		runBudgetLedgerFlush(ctx, store)
	})
	runtime.OnStart("health-checker", func(ctx context.Context) {
		healthChecker.Start(ctx)
	})
	runtime.OnShutdown("limiter", func(context.Context) error {
		limiterSvc.Stop()
		return nil
	})
	runtime.OnShutdown("eventbus", func(context.Context) error {
		err := persistBus.Close()
		if dropped := persistBus.Dropped(); dropped > 0 {
			slog.Warn("persistence event bus dropped events during run", "count", dropped)
		}
		return err
	})
	runtime.OnShutdown("provider-manager", func(context.Context) error {
		providerMgr.CloseIdleConnections()
		return nil
	})
	runtime.OnShutdown("plugin-manager", func(context.Context) error {
		if pluginManager != nil {
			return pluginManager.Close()
		}
		return nil
	})
	runtime.OnShutdown("guardrails", func(context.Context) error {
		if guardrails != nil {
			return guardrails.Close()
		}
		return nil
	})
	runtime.OnShutdown("middleware-plugin-manager", func(context.Context) error {
		if pm := httpMiddleware.PluginManager(); pm != nil {
			pm.Close()
		}
		return nil
	})

	slog.Info("gateway listening", "addr", cfg.Server.ListenAddr)
	return runtime.Run(ctx)
}

type responseTTLStore interface {
	DeleteResponsesOlderThan(context.Context, time.Time) (int64, error)
}

func runResponseTTLCleanup(ctx context.Context, store responseTTLStore) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	slog.Info("starting response TTL cleanup (30-day retention)")
	cleanupResponses(ctx, store)
	for {
		select {
		case <-ticker.C:
			slog.Info("running response TTL cleanup (30-day retention)")
			cleanupResponses(ctx, store)
		case <-ctx.Done():
			return
		}
	}
}

func cleanupResponses(ctx context.Context, store responseTTLStore) {
	deleted, err := store.DeleteResponsesOlderThan(ctx, time.Now().UTC().AddDate(0, 0, -30))
	if err != nil {
		slog.Error("response TTL cleanup failed", "error", err)
		return
	}
	if deleted > 0 {
		slog.Info("response TTL cleanup completed", "deleted", deleted)
	}
}

type budgetLedgerFlusher interface {
	FlushBudgetLedgerDeltas(context.Context) error
}

func runBudgetLedgerFlush(ctx context.Context, store budgetLedgerFlusher) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	flushBudgetLedger(ctx, store)
	for {
		select {
		case <-ticker.C:
			flushBudgetLedger(ctx, store)
		case <-ctx.Done():
			flushBudgetLedger(context.Background(), store)
			return
		}
	}
}

func flushBudgetLedger(ctx context.Context, store budgetLedgerFlusher) {
	if store == nil {
		return
	}
	if err := store.FlushBudgetLedgerDeltas(ctx); err != nil {
		slog.Error("budget ledger flush failed", "error", err)
	}
}
