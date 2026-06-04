package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/filter"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/limiter"
)

type MetricsRecorder interface {
	RecordError(surface, providerName, result, errorClass string)
}

// Middleware composes auth and guard middleware behind the legacy API.
type Middleware struct {
	auth  *AuthMiddleware
	guard *GuardMiddleware
	pm    *filter.PluginManager
}

func New(cfg *config.Config, store repository.Store, limiterSvc *limiter.Limiter, budgetSvc *budget.Service, alertSvc *alert.AlertService, metrics MetricsRecorder) *Middleware {
	authMW := NewAuthMiddleware(store, metrics)

	authSvc := authMW.Service()
	registry := filter.NewRegistry()
	registry.MustRegister("model_whitelist", func() (filter.Filter, error) {
		return filter.NewModelWhitelistFilter(authSvc), nil
	})
	registry.MustRegister("quota", func() (filter.Filter, error) {
		return filter.NewQuotaFilter(authSvc), nil
	})
	registry.MustRegister("budget", func() (filter.Filter, error) {
		return filter.NewBudgetFilter(budgetSvc, alertSvc), nil
	})
	registry.MustRegister("rate_limit", func() (filter.Filter, error) {
		return filter.NewRateLimitFilter(authSvc, limiterSvc), nil
	})

	var pm *filter.PluginManager
	// Load WASM plugins from directory if enabled.
	if cfg != nil && cfg.Plugins.Enabled {
		pm = filter.NewPluginManager(cfg.Plugins.Directory)
		if err := pm.LoadAll(); err == nil {
			pluginRegistry := pm.BuildRegistry()
			for _, name := range pluginRegistry.Names() {
				factory, _ := pluginRegistry.Get(name)
				_ = registry.Register(name, factory)
			}
		}
	}

	// Build pipeline: built-in filters first, then WASM plugins.
	filterNames := []string{"model_whitelist", "quota", "budget", "rate_limit"}
	for _, name := range registry.Names() {
		if name != "model_whitelist" && name != "quota" && name != "budget" && name != "rate_limit" {
			filterNames = append(filterNames, name)
		}
	}
	pipeline, err := registry.BuildPipeline(filterNames)
	if err != nil {
		panic(err)
	}

	return &Middleware{
		auth:  authMW,
		guard: NewGuardMiddleware(pipeline, metrics),
		pm:    pm,
	}
}

// PluginManager returns the WASM plugin manager, or nil if plugins are disabled.
func (m *Middleware) PluginManager() *filter.PluginManager {
	return m.pm
}

func (m *Middleware) AuthService() *auth.Auth {
	return m.auth.Service()
}

func (m *Middleware) Auth() gin.HandlerFunc {
	return m.auth.Auth()
}

func (m *Middleware) RequireRoles(roles ...string) gin.HandlerFunc {
	return m.auth.RequireRoles(roles...)
}

func (m *Middleware) GuardLLMRequest() gin.HandlerFunc {
	return m.guard.GuardLLMRequest()
}

func (m *Middleware) AdminAuth() gin.HandlerFunc {
	return m.auth.AdminAuth()
}

func (m *Middleware) AdminRequireRoles(roles ...string) gin.HandlerFunc {
	return m.auth.AdminRequireRoles(roles...)
}
