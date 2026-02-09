package router

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"gateyes/internal/config"
	"gateyes/internal/gateway"
	"gateyes/internal/middleware"
)

func New(cfg *config.Config, metrics *middleware.Metrics) (http.Handler, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}
	if metrics == nil {
		metrics = middleware.NewMetrics(cfg.Metrics.Namespace)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)

	if cfg.Metrics.Enabled {
		path := cfg.Metrics.Path
		if path == "" {
			path = "/metrics"
		}
		mux.Handle(path, metrics.Handler())
	}

	openaiProxy, err := gateway.NewOpenAIProxy(cfg.Gateway, cfg.Providers)
	if err != nil {
		return nil, err
	}
	registerPrefix(mux, normalizePrefix(cfg.Gateway.OpenAIPathPrefix), openaiProxy)

	if cfg.Gateway.AnthropicPathPrefix != "" {
		anthropicProxy, err := gateway.NewAnthropicProxy(cfg.Gateway, cfg.Providers)
		if err != nil {
			return nil, err
		}
		registerPrefix(mux, normalizePrefix(cfg.Gateway.AnthropicPathPrefix), anthropicProxy)
	}

	if cfg.Gateway.AgentToProdUpstream != "" {
		proxy, err := gateway.NewStaticProxy(cfg.Gateway.AgentToProdUpstream, cfg.Gateway.AgentToProdPrefix)
		if err != nil {
			return nil, err
		}
		registerPrefix(mux, normalizePrefix(cfg.Gateway.AgentToProdPrefix), proxy)
	}

	if cfg.Gateway.AgentToMcpUpstream != "" {
		// Use guarded proxy for MCP with protection
		guardConfig := convertMCPGuardConfig(cfg.Gateway.MCPGuard)
		proxy, err := gateway.NewStaticProxyWithGuard(
			cfg.Gateway.AgentToMcpUpstream,
			cfg.Gateway.AgentToMcpPrefix,
			guardConfig,
		)
		if err != nil {
			return nil, err
		}
		registerPrefix(mux, normalizePrefix(cfg.Gateway.AgentToMcpPrefix), proxy)

		// Add MCP stats endpoint if guard is enabled
		if cfg.Gateway.MCPGuard.Enabled {
			mux.HandleFunc("/mcp-stats", func(w http.ResponseWriter, r *http.Request) {
				stats := proxy.GetMetrics()
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(stats)
			})
		}
	}

	rateLimiter := middleware.NewRateLimiter(cfg.RateLimit, cfg.Auth)
	quota := middleware.NewQuota(cfg.Quota, cfg.Auth)

	// Initialize cache middleware
	cacheMiddleware, err := middleware.NewCacheMiddleware(convertCacheConfig(cfg.Cache))
	if err != nil {
		return nil, err
	}

	// Add cache stats endpoint if enabled
	if cfg.Cache.Enabled {
		mux.HandleFunc("/cache-stats", func(w http.ResponseWriter, r *http.Request) {
			stats := cacheMiddleware.GetStats()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(stats)
		})
	}

	handler := middleware.Chain(
		mux,
		middleware.Logging(),
		metrics.Middleware(cfg.Metrics.Enabled),
		cacheMiddleware.Middleware(),
		rateLimiter.Middleware(),
		quota.Middleware(),
		middleware.Auth(cfg.Auth),
		middleware.Policy(cfg.Policy),
	)

	return handler, nil
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
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

func registerPrefix(mux *http.ServeMux, prefix string, handler http.Handler) {
	if prefix == "/" {
		mux.Handle("/", handler)
		return
	}
	mux.Handle(prefix+"/", handler)
	mux.Handle(prefix, handler)
}
