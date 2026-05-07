package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/controllers"
	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/discovery"
	"github.com/gateyes/gateway/internal/handler"
	"github.com/gateyes/gateway/internal/ingress"
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/proxy"
	redispkg "github.com/gateyes/gateway/internal/redis"
	"github.com/redis/go-redis/v9"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
	"github.com/gateyes/gateway/internal/service/router"
	"github.com/gateyes/gateway/api/v1alpha1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
	networkingv1 "k8s.io/api/networking/v1"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	slog.SetDefault(slog.New(middleware.NewTraceHandler(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))))

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

	// Redis for distributed rate limiting and alert dedup
	var redisClient *redis.Client
	if cfg.Redis.Enabled() {
		var err error
		redisClient, err = redispkg.NewClient(cfg.Redis)
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

	httpMiddleware := middleware.New(store, limiterSvc, budgetSvc, alertSvc, metrics)
	responsesService := responseSvc.New(&responseSvc.Dependencies{
		Config:      cfg,
		Store:       store,
		Auth:        httpMiddleware.AuthService(),
		ProviderMgr: providerMgr,
		Router:      routerSvc,
		Alert:       alertSvc,
		Limiter:     limiterSvc,
		Cache:       cacheSvc,
		Metrics:     metrics,
	})
	catalogSvc := catalog.New(&catalog.Dependencies{
		Store:     store,
		Auth:      httpMiddleware.AuthService(),
		Limiter:   limiterSvc,
		BudgetSvc: budgetSvc,
		AlertSvc:  alertSvc,
		Responses: responsesService,
	})

	h := handler.NewHandler(&handler.Dependencies{
		Config:      cfg,
		Store:       store,
		Metrics:     metrics,
		ProviderMgr: providerMgr,
		ResponseSvc: responsesService,
		CatalogSvc:  catalogSvc,
	})

	// Ingress + Discovery + Proxy + TLS setup.
	var (
		discoveryReg      *discovery.Registry
		routeTable        *ingress.RouteTable
		ingressProxy      *proxy.Proxy
		ingressMiddleware *ingress.Middleware
		tlsManager        *ingress.TLSManager
	)
	if cfg.Ingress.Enabled {
		discoveryReg = discovery.NewRegistry(cfg.Discovery.Type)
		if cfg.Discovery.Type == "kubernetes" || cfg.Discovery.Type == "" {
			// K8s discovery will be registered after mgr is created.
		}
		// Always register static discovery as fallback.
		discoveryReg.Register("static", discovery.NewStaticDiscovery())

		ingressProxy = proxy.NewProxy(proxy.ProxyConfig{
			ConnectTimeout:  time.Duration(cfg.Proxy.ConnectTimeout) * time.Second,
			ReadTimeout:     time.Duration(cfg.Proxy.ReadTimeout) * time.Second,
			SendTimeout:     time.Duration(cfg.Proxy.SendTimeout) * time.Second,
			IdleConnTimeout: time.Duration(cfg.Proxy.IdleConnTimeout) * time.Second,
			MaxIdleConns:    cfg.Proxy.MaxIdleConns,
			MaxConnsPerHost: cfg.Proxy.MaxConnsPerHost,
		})
		routeTable = ingress.NewRouteTable()
		ingressMiddleware = ingress.NewMiddleware(ingress.MiddlewareOpts{
			RouteTable: routeTable,
			Proxy:      ingressProxy,
			Enabled:    true,
		})
	}

	adminHandler := handler.NewAdminHandler(store, providerMgr, catalogSvc, reloader)
	adminHandler.SetHealthChecker(healthChecker)
	srv := handler.NewServer(cfg.Server, h, adminHandler, httpMiddleware, ingressMiddleware, nil)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start K8s controller-runtime for Provider CRD and Ingress reconciliation.
	mgr, tlsManager, err := setupControllerManager(providerMgr, store, discoveryReg, routeTable, cfg.Ingress)
	if err != nil {
		slog.Warn("controller manager not started", "error", err)
	} else {
		if tlsManager != nil {
			srv.SetTLSManager(tlsManager)
		}
		go func() {
			if err := mgr.Start(ctx); err != nil {
				slog.Error("controller manager stopped", "error", err)
			}
		}()
		slog.Info("controller manager started")
		if cfg.Admin.CRDMode != nil {
			adminHandler.SetCRDMode(*cfg.Admin.CRDMode)
		} else {
			adminHandler.SetCRDMode(true)
		}
	}

	go metrics.StartProviderStatsExporter(ctx, providerMgr.Stats, 5*time.Second)

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
	providerMgr.CloseIdleConnections()
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

func seedTenantProviders(ctx context.Context, store repository.TenantStore, tenantID string, names []string) error {
	existing, err := store.ListTenantProviders(ctx, tenantID)
	if err != nil {
		return err
	}
	merged := append([]string(nil), existing...)
	seen := make(map[string]struct{}, len(existing))
	for _, name := range existing {
		seen[name] = struct{}{}
	}
	for _, name := range names {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		merged = append(merged, name)
	}
	return store.ReplaceTenantProviders(ctx, tenantID, merged)
}

func seedProviderRegistry(ctx context.Context, store repository.ProviderRegistryStore, providers []config.ProviderConfig) error {
	for _, item := range providers {
		existing, err := store.GetProviderRegistry(ctx, item.Name)
		if err == nil {
			if existing.RuntimeConfig != nil {
				continue
			}
		} else if err != repository.ErrNotFound {
			return err
		}
		if err := store.UpsertProviderRegistry(ctx, provider.DefaultRegistryRecordFromConfig(item)); err != nil {
			return err
		}
	}
	return nil
}

// setupControllerManager initializes the controller-runtime manager for Provider CRD and Ingress reconciliation.
// Returns nil, nil when K8s is not available (e.g. local development).
func setupControllerManager(providerMgr *provider.Manager, store repository.ProviderRegistryStore, discoveryReg *discovery.Registry, routeTable *ingress.RouteTable, ingressCfg config.IngressConfig) (ctrl.Manager, *ingress.TLSManager, error) {
	kubeCfg, err := ctrlconfig.GetConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("get kubeconfig: %w", err)
	}

	mgr, err := ctrl.NewManager(kubeCfg, ctrl.Options{})
	if err != nil {
		return nil, nil, fmt.Errorf("create manager: %w", err)
	}

	if err := v1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, nil, fmt.Errorf("add scheme: %w", err)
	}
	if err := networkingv1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, nil, fmt.Errorf("add networking scheme: %w", err)
	}

	if err := (&controllers.ProviderReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ProviderMgr: providerMgr,
		Store:       store,
	}).SetupWithManager(mgr); err != nil {
		return nil, nil, fmt.Errorf("setup provider reconciler: %w", err)
	}

	var tlsManager *ingress.TLSManager
	if ingressCfg.Enabled && discoveryReg != nil && routeTable != nil {
		discoveryReg.Register("kubernetes", discovery.NewK8sEndpointsDiscovery(mgr.GetClient(), ingressCfg.WatchNamespace))
		if ingressCfg.TLSEnabled {
			tlsManager = ingress.NewTLSManager(mgr.GetClient(), ingressCfg.TLSSecretNamespace)
		}
		if err := (&ingress.Controller{
			Client:     mgr.GetClient(),
			Scheme:     mgr.GetScheme(),
			RouteTable: routeTable,
			Discovery:  discoveryReg,
			TLSManager: tlsManager,
			Config:     ingressCfg,
			PodIP:      os.Getenv("POD_IP"),
		}).SetupWithManager(mgr); err != nil {
			return nil, nil, fmt.Errorf("setup ingress reconciler: %w", err)
		}
		slog.Info("ingress controller registered", "class", ingressCfg.Class, "tls", ingressCfg.TLSEnabled)
	}

	return mgr, tlsManager, nil
}
