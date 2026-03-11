package bootstrap

import (
	"errors"

	"gateyes/internal/cache"
	"gateyes/internal/config"
	"gateyes/internal/handler"
	"gateyes/internal/middleware"
	providerupstream "gateyes/internal/provider/upstream"
	"gateyes/internal/router"
	"gateyes/internal/server"
	"gateyes/internal/service/gateway"

	"github.com/gin-gonic/gin"
)

type App struct {
	Config  *config.Config
	Metrics *middleware.Metrics
	Engine  *gin.Engine
	Server  *server.Server
}

func New(cfg *config.Config) (*App, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	metrics := middleware.NewMetricsFromConfig(cfg.Metrics)
	engine, err := NewEngine(cfg, metrics)
	if err != nil {
		return nil, err
	}

	return &App{
		Config:  cfg,
		Metrics: metrics,
		Engine:  engine,
		Server:  server.New(cfg, engine),
	}, nil
}

func NewEngine(cfg *config.Config, metrics *middleware.Metrics) (*gin.Engine, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if metrics == nil {
		metrics = middleware.NewMetricsFromConfig(cfg.Metrics)
	}

	middlewares, cacheMiddleware, err := buildMiddlewares(cfg, metrics)
	if err != nil {
		return nil, err
	}

	operations := handler.NewOperationalHandler(cacheMiddleware)
	openAIService, err := gateway.New(cfg.Gateway, cfg.Auth, cfg.Providers)
	if err != nil {
		return nil, err
	}

	proxyRoutes := []router.ProxyRoute{
		{
			Prefix:  cfg.Gateway.OpenAIPathPrefix,
			Handler: handler.NewHTTPBridge(openAIService).Handle,
		},
	}

	if cfg.Gateway.AgentToProdUpstream != "" {
		passthrough, err := providerupstream.New(
			cfg.Gateway.AgentToProdUpstream,
			"",
			nil,
			"",
			"",
			"",
			cfg.Gateway.AgentToProdPrefix,
		)
		if err != nil {
			return nil, err
		}
		proxyRoutes = append(proxyRoutes, router.ProxyRoute{
			Prefix:  cfg.Gateway.AgentToProdPrefix,
			Handler: handler.NewHTTPBridge(passthrough).Handle,
		})
	}

	deps := router.Dependencies{
		Middlewares:    middlewares,
		HealthHandler:  operations.Health,
		ProxyRoutes:    proxyRoutes,
		MetricsPath:    cfg.Metrics.Path,
		MetricsHandler: metrics.Handler(),
	}
	if !cfg.Metrics.Enabled {
		deps.MetricsHandler = nil
	}
	if cfg.Cache.Enabled {
		deps.CacheStatsHandler = operations.CacheStats
	}

	return router.New(deps), nil
}

func buildMiddlewares(
	cfg *config.Config,
	metrics *middleware.Metrics,
) ([]gin.HandlerFunc, *middleware.CacheMiddleware, error) {
	rateLimiter := middleware.NewRateLimiter(cfg.RateLimit, cfg.Auth)
	if cfg.RateLimit.Enabled {
		if err := rateLimiter.InitError(); err != nil {
			return nil, nil, err
		}
	}

	quota := middleware.NewQuota(cfg.Quota, cfg.Auth)
	if cfg.Quota.Enabled {
		if err := quota.InitError(); err != nil {
			return nil, nil, err
		}
	}

	cacheMiddleware, err := middleware.NewCacheMiddleware(convertCacheConfig(cfg.Cache))
	if err != nil {
		return nil, nil, err
	}

	handlers := []gin.HandlerFunc{
		middleware.ToGinMiddleware(middleware.SanitizeInternalHeaders()),
		middleware.ToGinMiddleware(middleware.Logging()),
		middleware.ToGinMiddleware(metrics.Middleware(cfg.Metrics.Enabled)),
		middleware.ToGinMiddleware(cacheMiddleware.Middleware()),
		middleware.ToGinMiddleware(middleware.Auth(cfg.Auth)),
		middleware.ToGinMiddleware(rateLimiter.Middleware()),
		middleware.ToGinMiddleware(quota.Middleware()),
	}

	return handlers, cacheMiddleware, nil
}

func convertCacheConfig(cfg config.CacheConfig) cache.CacheConfig {
	return cache.CacheConfig{
		Enabled:       cfg.Enabled,
		Backend:       cfg.Backend,
		TTL:           cfg.TTL.Duration,
		MaxSize:       cfg.MaxSize,
		MaxEntries:    cfg.MaxEntries,
		KeyStrategy:   cfg.KeyStrategy,
		RedisAddr:     cfg.RedisAddr,
		RedisPassword: cfg.RedisPassword,
		RedisDB:       cfg.RedisDB,
	}
}
