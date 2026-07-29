package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/app/config"
	mw "github.com/gateyes/gateway/internal/handler/middleware"
)

var ErrServerClosed = fmt.Errorf("server closed")

type Server struct {
	cfg    config.ServerConfig
	engine *gin.Engine
	srv    *http.Server
}

func NewServer(cfg config.ServerConfig, h *Handler, adminH *AdminHandler, mwSvc *mw.Middleware) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(mw.Correlation())
	engine.Use(mw.OtelTrace())
	engine.Use(gin.Logger())
	engine.Use(gin.Recovery())

	engine.GET("/health", h.Health)
	engine.GET("/ready", h.Ready)
	engine.GET("/metrics", h.Metrics)

	v1 := engine.Group("/v1")
	v1.Use(mwSvc.Auth())
	{
		v1.GET("/responses/:id", h.GetResponse)
		v1.GET("/models", h.Models)
	}

	llm := v1.Group("")
	llm.Use(mwSvc.GuardLLMRequest())
	{
		llm.POST("/responses", h.Responses)
		llm.POST("/chat/completions", h.Chat)
		llm.POST("/messages", h.AnthropicMessages)
		llm.POST("/embeddings", h.Embeddings)
		llm.POST("/images/generations", h.ImageGenerations)
	}

	serviceRoutes := engine.Group("/service/:prefix")
	serviceRoutes.Use(mwSvc.Auth())
	{
		serviceRoutes.POST("/responses", h.ServiceResponses)
		serviceRoutes.POST("/chat/completions", h.ServiceChat)
		serviceRoutes.POST("/messages", h.ServiceMessages)
		serviceRoutes.POST("/invoke", h.ServiceInvoke)
	}

	// Admin API v1 (primary)
	adminV1 := engine.Group("/admin/v1")
	adminV1.Use(func(c *gin.Context) { c.Set("middleware", mwSvc); c.Next() })
	adminV1.Use(mwSvc.AdminAuth())
	registerAdminRoutes(adminV1, adminH, mwSvc)

	// Admin legacy compatibility alias (/admin/* → /admin/v1/*)
	adminLegacy := engine.Group("/admin")
	adminLegacy.Use(func(c *gin.Context) { c.Set("middleware", mwSvc); c.Next() })
	adminLegacy.Use(mwSvc.AdminAuth())
	registerAdminRoutes(adminLegacy, adminH, mwSvc)

	// Public admin auth endpoints (OIDC login/callback/refresh do not require AdminAuth).
	adminAuth := engine.Group("/admin/auth")
	adminAuth.Use(func(c *gin.Context) { c.Set("middleware", mwSvc); c.Next() })
	{
		adminAuth.GET("/oidc/status", adminH.OIDCStatus)
		adminAuth.GET("/oidc/login", adminH.OIDCLogin)
		adminAuth.GET("/callback", adminH.OIDCCallback)
		adminAuth.POST("/refresh", adminH.OIDCRefresh)
		adminAuth.POST("/logout", mwSvc.AdminAuth(), adminH.OIDCLogout)
	}

	// Tenant routes under v1
	tenantsV1 := adminV1.Group("/tenants")
	tenantsV1.Use(mwSvc.RequirePermission(mw.PermTenantWrite))
	registerTenantRoutes(tenantsV1, adminH, mwSvc)

	// Tenant routes under legacy
	tenantsLegacy := adminLegacy.Group("/tenants")
	tenantsLegacy.Use(mwSvc.RequirePermission(mw.PermTenantWrite))
	registerTenantRoutes(tenantsLegacy, adminH, mwSvc)

	s := &Server{cfg: cfg, engine: engine}
	return s
}

func registerAdminRoutes(g *gin.RouterGroup, adminH *AdminHandler, mwSvc *mw.Middleware) {
	g.GET("/dashboard", mwSvc.RequirePermission(mw.PermUsageRead), adminH.Dashboard)
	g.GET("/cache/summary", mwSvc.RequirePermission(mw.PermUsageRead), adminH.GetCacheSummary)
	g.GET("/providers", mwSvc.RequirePermission(mw.PermProviderRead), adminH.GetProviders)
	g.POST("/providers/check", mwSvc.RequirePermission(mw.PermProviderRead), adminH.CheckProviders)
	g.POST("/providers", mwSvc.RequirePermission(mw.PermProviderWrite), adminH.CreateProvider)
	g.GET("/providers/:name", mwSvc.RequirePermission(mw.PermProviderRead), adminH.GetProvider)
	g.GET("/providers/:name/stats", mwSvc.RequirePermission(mw.PermProviderRead), adminH.GetProviderStats)
	g.PUT("/providers/:name", mwSvc.RequirePermission(mw.PermProviderWrite), adminH.UpdateProvider)
	g.DELETE("/providers/:name", mwSvc.RequirePermission(mw.PermProviderWrite), adminH.DeleteProvider)
	g.GET("/audit", mwSvc.RequirePermission(mw.PermAuditRead), adminH.ListAuditLogs)
	g.GET("/services", mwSvc.RequirePermission(mw.PermServiceRead), adminH.ListServices)
	g.POST("/services", mwSvc.RequirePermission(mw.PermServiceWrite), adminH.CreateService)
	g.GET("/services/:id", mwSvc.RequirePermission(mw.PermServiceRead), adminH.GetService)
	g.PUT("/services/:id", mwSvc.RequirePermission(mw.PermServiceWrite), adminH.UpdateService)
	g.DELETE("/services/:id", mwSvc.RequirePermission(mw.PermServiceWrite), adminH.DeleteService)
	g.GET("/services/:id/versions", mwSvc.RequirePermission(mw.PermServiceRead), adminH.ListServiceVersions)
	g.POST("/services/:id/versions", mwSvc.RequirePermission(mw.PermServiceWrite), adminH.CreateServiceVersion)
	g.POST("/services/:id/publish", mwSvc.RequirePermission(mw.PermServiceWrite), adminH.PublishServiceVersion)
	g.POST("/services/:id/promote", mwSvc.RequirePermission(mw.PermServiceWrite), adminH.PromoteStagedServiceVersion)
	g.POST("/services/:id/rollback", mwSvc.RequirePermission(mw.PermServiceWrite), adminH.RollbackServiceVersion)
	g.GET("/services/:id/subscriptions", mwSvc.RequirePermission(mw.PermServiceRead), adminH.ListServiceSubscriptions)
	g.POST("/services/:id/subscriptions", mwSvc.RequirePermission(mw.PermServiceWrite), adminH.CreateServiceSubscription)
	g.GET("/subscriptions/:id", mwSvc.RequirePermission(mw.PermServiceRead), adminH.GetServiceSubscription)
	g.POST("/subscriptions/:id/review", mwSvc.RequirePermission(mw.PermServiceWrite), adminH.ReviewServiceSubscription)
	g.GET("/plugins", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.ListPlugins)
	g.POST("/plugins", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.CreatePlugin)
	g.POST("/plugins/upload", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.UploadPlugin)
	g.GET("/plugins/:id", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.GetPlugin)
	g.PUT("/plugins/:id", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.UpdatePlugin)
	g.DELETE("/plugins/:id", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.DeletePlugin)
	g.GET("/keys", mwSvc.RequirePermission(mw.PermAPIKeyRead), adminH.ListAPIKeys)
	g.POST("/keys", mwSvc.RequirePermission(mw.PermAPIKeyWrite), adminH.CreateAPIKey)
	g.GET("/keys/:id", mwSvc.RequirePermission(mw.PermAPIKeyRead), adminH.GetAPIKey)
	g.PUT("/keys/:id", mwSvc.RequirePermission(mw.PermAPIKeyWrite), adminH.UpdateAPIKey)
	g.POST("/keys/:id/rotate", mwSvc.RequirePermission(mw.PermAPIKeyWrite), adminH.RotateAPIKey)
	g.POST("/keys/:id/revoke", mwSvc.RequirePermission(mw.PermAPIKeyWrite), adminH.RevokeAPIKey)
	g.GET("/virtual-keys", mwSvc.RequirePermission(mw.PermVirtualKeyRead), adminH.ListVirtualKeys)
	g.POST("/virtual-keys", mwSvc.RequirePermission(mw.PermVirtualKeyWrite), adminH.CreateVirtualKey)
	g.GET("/virtual-keys/:id", mwSvc.RequirePermission(mw.PermVirtualKeyRead), adminH.GetVirtualKey)
	g.PUT("/virtual-keys/:id", mwSvc.RequirePermission(mw.PermVirtualKeyWrite), adminH.UpdateVirtualKey)
	g.DELETE("/virtual-keys/:id", mwSvc.RequirePermission(mw.PermVirtualKeyWrite), adminH.DeleteVirtualKey)
	g.GET("/users", mwSvc.RequirePermission(mw.PermUserRead), adminH.ListUsers)
	g.POST("/users", mwSvc.RequirePermission(mw.PermUserWrite), adminH.CreateUser)
	g.GET("/users/:id", mwSvc.RequirePermission(mw.PermUserRead), adminH.GetUser)
	g.PUT("/users/:id", mwSvc.RequirePermission(mw.PermUserWrite), adminH.UpdateUser)
	g.DELETE("/users/:id", mwSvc.RequirePermission(mw.PermUserWrite), adminH.DeleteUser)
	g.POST("/users/:id/reset", mwSvc.RequirePermission(mw.PermUserWrite), adminH.ResetUserUsage)
	g.GET("/users/:id/usage", mwSvc.RequirePermission(mw.PermUsageRead), adminH.GetUserUsage)
	g.GET("/projects", mwSvc.RequirePermission(mw.PermProjectRead), adminH.ListProjects)
	g.POST("/projects", mwSvc.RequirePermission(mw.PermProjectWrite), adminH.CreateProject)
	g.GET("/projects/:id", mwSvc.RequirePermission(mw.PermProjectRead), adminH.GetProject)
	g.GET("/projects/:id/usage", mwSvc.RequirePermission(mw.PermUsageRead), adminH.GetProjectUsage)
	g.PUT("/projects/:id", mwSvc.RequirePermission(mw.PermProjectWrite), adminH.UpdateProject)
	g.DELETE("/projects/:id", mwSvc.RequirePermission(mw.PermProjectWrite), adminH.DeleteProject)
	g.GET("/responses", mwSvc.RequirePermission(mw.PermResponseRead), adminH.ListResponses)
	g.GET("/responses/:id", mwSvc.RequirePermission(mw.PermResponseRead), adminH.GetResponseDetail)
	g.GET("/responses/:id/trace", mwSvc.RequirePermission(mw.PermResponseRead), adminH.GetResponseTrace)
	g.GET("/budgets", mwSvc.RequirePermission(mw.PermBudgetRead), adminH.GetBudgets)
	g.GET("/usage/summary", mwSvc.RequirePermission(mw.PermUsageRead), adminH.GetUsageSummary)
	g.GET("/usage/breakdown", mwSvc.RequirePermission(mw.PermUsageRead), adminH.GetUsageBreakdown)
	g.GET("/usage/trend", mwSvc.RequirePermission(mw.PermUsageRead), adminH.GetUsageTrend)
	g.POST("/reload", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.ReloadConfig)
	g.GET("/roles", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.ListRoles)
	g.POST("/roles", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.CreateRole)
	g.GET("/roles/:id", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.GetRole)
	g.PUT("/roles/:id", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.UpdateRole)
	g.DELETE("/roles/:id", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.DeleteRole)
	g.GET("/permissions", mwSvc.RequirePermission(mw.PermConfigWrite), adminH.ListPermissions)
}

func registerTenantRoutes(g *gin.RouterGroup, adminH *AdminHandler, mwSvc *mw.Middleware) {
	g.GET("", mwSvc.RequirePermission(mw.PermTenantRead), adminH.ListTenants)
	g.POST("", mwSvc.RequirePermission(mw.PermTenantWrite), adminH.CreateTenant)
	g.GET("/:id", mwSvc.RequirePermission(mw.PermTenantRead), adminH.GetTenant)
	g.PUT("/:id", mwSvc.RequirePermission(mw.PermTenantWrite), adminH.UpdateTenant)
	g.DELETE("/:id", mwSvc.RequirePermission(mw.PermTenantWrite), adminH.DeleteTenant)
	g.POST("/:id/providers", mwSvc.RequirePermission(mw.PermTenantWrite), adminH.ReplaceTenantProviders)
}

func (s *Server) Start() error {
	s.srv = s.buildHTTPServer()
	if s.cfg.TLSCertFile != "" && s.cfg.TLSKeyFile != "" {
		return s.srv.ListenAndServeTLS(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
	}
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return fmt.Errorf("server not started")
	}
	return s.srv.Shutdown(ctx)
}

func (s *Server) buildHTTPServer() *http.Server {
	return &http.Server{
		Addr:         s.cfg.ListenAddr,
		Handler:      s.engine,
		ReadTimeout:  time.Duration(s.cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.cfg.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(s.cfg.IdleTimeout) * time.Second,
	}
}
