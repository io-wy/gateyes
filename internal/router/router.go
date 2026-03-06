package router

import (
	"errors"
	"net/http"
	"strings"

	"gateyes/internal/config"
	"gateyes/internal/gateway"
	"gateyes/internal/middleware"

	"github.com/gin-gonic/gin"
)

func New(cfg *config.Config, metrics *middleware.Metrics) (*gin.Engine, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if metrics == nil {
		metrics = middleware.NewMetrics(cfg.Metrics.Namespace)
	}

	engine := gin.New()
	engine.Use(gin.Recovery())

	rateLimiter := middleware.NewRateLimiter(cfg.RateLimit, cfg.Auth)
	quota := middleware.NewQuota(cfg.Quota, cfg.Auth)

	cacheMiddleware, err := middleware.NewCacheMiddleware(convertCacheConfig(cfg.Cache))
	if err != nil {
		return nil, err
	}

	engine.Use(
		middleware.ToGinMiddleware(middleware.Logging()),
		middleware.ToGinMiddleware(metrics.Middleware(cfg.Metrics.Enabled)),
		middleware.ToGinMiddleware(cacheMiddleware.Middleware()),
		middleware.ToGinMiddleware(middleware.Auth(cfg.Auth)),
		middleware.ToGinMiddleware(rateLimiter.Middleware()),
		middleware.ToGinMiddleware(quota.Middleware()),
		middleware.ToGinMiddleware(middleware.Policy(cfg.Policy)),
	)

	engine.GET("/healthz", healthHandler)

	if cfg.Metrics.Enabled {
		path := cfg.Metrics.Path
		if path == "" {
			path = "/metrics"
		}
		engine.GET(path, gin.WrapH(metrics.Handler()))
	}

	if cfg.Cache.Enabled {
		engine.GET("/cache-stats", func(c *gin.Context) {
			stats := cacheMiddleware.GetStats()
			c.JSON(http.StatusOK, stats)
		})
	}

	openaiProxy, err := gateway.NewOpenAIProxy(cfg.Gateway, cfg.Auth, cfg.Providers)
	if err != nil {
		return nil, err
	}
	registerProxy(engine, normalizePrefix(cfg.Gateway.OpenAIPathPrefix), openaiProxy)

	if cfg.Gateway.AgentToProdUpstream != "" {
		proxy, err := gateway.NewStaticProxy(cfg.Gateway.AgentToProdUpstream, cfg.Gateway.AgentToProdPrefix)
		if err != nil {
			return nil, err
		}
		registerProxy(engine, normalizePrefix(cfg.Gateway.AgentToProdPrefix), proxy)
	}

	return engine, nil
}

func healthHandler(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}

func normalizePrefix(prefix string) string {
	clean := strings.TrimSpace(prefix)
	if clean == "" {
		return "/"
	}
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	clean = strings.TrimSuffix(clean, "/")
	if clean == "" {
		return "/"
	}
	return clean
}

func registerProxy(engine *gin.Engine, prefix string, handler http.Handler) {
	ginHandler := gin.WrapH(handler)

	if prefix == "/" {
		engine.Any("/", ginHandler)
		engine.Any("/*proxyPath", ginHandler)
		return
	}
	engine.Any(prefix, ginHandler)
	engine.Any(prefix+"/*proxyPath", ginHandler)
}
