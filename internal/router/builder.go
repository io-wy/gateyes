package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Dependencies struct {
	Middlewares       []gin.HandlerFunc
	HealthHandler     gin.HandlerFunc
	MetricsPath       string
	MetricsHandler    http.Handler
	CacheStatsHandler gin.HandlerFunc
	ProxyRoutes       []ProxyRoute
}

type ProxyRoute struct {
	Prefix  string
	Handler gin.HandlerFunc
}

func New(deps Dependencies) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.Recovery())

	for _, middleware := range deps.Middlewares {
		if middleware == nil {
			continue
		}
		engine.Use(middleware)
	}

	registerOperationalRoutes(engine, deps)
	registerProxyRoutes(engine, deps.ProxyRoutes)

	return engine
}

func registerOperationalRoutes(engine *gin.Engine, deps Dependencies) {
	if deps.HealthHandler != nil {
		engine.GET("/healthz", deps.HealthHandler)
	}

	if deps.MetricsHandler != nil {
		path := deps.MetricsPath
		if path == "" {
			path = "/metrics"
		}
		engine.GET(path, gin.WrapH(deps.MetricsHandler))
	}

	if deps.CacheStatsHandler != nil {
		engine.GET("/cache-stats", deps.CacheStatsHandler)
	}
}

func registerProxyRoutes(engine *gin.Engine, routes []ProxyRoute) {
	for _, route := range routes {
		if route.Handler == nil {
			continue
		}
		registerProxy(engine, normalizePrefix(route.Prefix), route.Handler)
	}
}
