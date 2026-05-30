package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/middleware"
)

var ErrServerClosed = fmt.Errorf("server closed")

type Server struct {
	cfg    config.ServerConfig
	engine *gin.Engine
	srv    *http.Server
}

func NewServer(cfg config.ServerConfig, h *Handler, adminH *AdminHandler, mw *middleware.Middleware) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(middleware.Correlation())
	engine.Use(middleware.OtelTrace())
	engine.Use(gin.Logger())
	engine.Use(gin.Recovery())

	engine.GET("/health", h.Health)
	engine.GET("/ready", h.Ready)
	engine.GET("/metrics", h.Metrics)

	v1 := engine.Group("/v1")
	v1.Use(mw.Auth())
	{
		v1.GET("/responses/:id", h.GetResponse)
		v1.GET("/models", h.Models)
	}

	llm := v1.Group("")
	llm.Use(mw.GuardLLMRequest())
	{
		llm.POST("/responses", h.Responses)
		llm.POST("/chat/completions", h.Chat)
		llm.POST("/messages", h.AnthropicMessages)
		llm.POST("/embeddings", h.Embeddings)
	}

	serviceRoutes := engine.Group("/service/:prefix")
	serviceRoutes.Use(mw.Auth())
	{
		serviceRoutes.POST("/responses", h.ServiceResponses)
		serviceRoutes.POST("/chat/completions", h.ServiceChat)
		serviceRoutes.POST("/messages", h.ServiceMessages)
		serviceRoutes.POST("/invoke", h.ServiceInvoke)
	}

	// Admin API v1 (primary)
	adminV1 := engine.Group("/admin/v1")
	adminV1.Use(mw.AdminAuth())
	registerAdminRoutes(adminV1, adminH)

	// Admin legacy compatibility alias (/admin/* → /admin/v1/*)
	adminLegacy := engine.Group("/admin")
	adminLegacy.Use(mw.AdminAuth())
	registerAdminRoutes(adminLegacy, adminH)

	// Tenant routes under v1
	tenantsV1 := adminV1.Group("/tenants")
	tenantsV1.Use(middleware.RequirePermission(middleware.PermTenantWrite))
	registerTenantRoutes(tenantsV1, adminH)

	// Tenant routes under legacy
	tenantsLegacy := adminLegacy.Group("/tenants")
	tenantsLegacy.Use(middleware.RequirePermission(middleware.PermTenantWrite))
	registerTenantRoutes(tenantsLegacy, adminH)

	s := &Server{cfg: cfg, engine: engine}
	return s
}

func registerAdminRoutes(g *gin.RouterGroup, adminH *AdminHandler) {
	g.GET("/dashboard", middleware.RequirePermission(middleware.PermUsageRead), adminH.Dashboard)
	g.GET("/providers", middleware.RequirePermission(middleware.PermProviderRead), adminH.GetProviders)
	g.POST("/providers/check", middleware.RequirePermission(middleware.PermProviderRead), adminH.CheckProviders)
	g.POST("/providers", middleware.RequirePermission(middleware.PermProviderWrite), adminH.CreateProvider)
	g.GET("/providers/:name", middleware.RequirePermission(middleware.PermProviderRead), adminH.GetProvider)
	g.GET("/providers/:name/stats", middleware.RequirePermission(middleware.PermProviderRead), adminH.GetProviderStats)
	g.PUT("/providers/:name", middleware.RequirePermission(middleware.PermProviderWrite), adminH.UpdateProvider)
	g.DELETE("/providers/:name", middleware.RequirePermission(middleware.PermProviderWrite), adminH.DeleteProvider)
	g.GET("/audit", middleware.RequirePermission(middleware.PermAuditRead), adminH.ListAuditLogs)
	g.GET("/services", middleware.RequirePermission(middleware.PermServiceRead), adminH.ListServices)
	g.POST("/services", middleware.RequirePermission(middleware.PermServiceWrite), adminH.CreateService)
	g.GET("/services/:id", middleware.RequirePermission(middleware.PermServiceRead), adminH.GetService)
	g.PUT("/services/:id", middleware.RequirePermission(middleware.PermServiceWrite), adminH.UpdateService)
	g.GET("/services/:id/versions", middleware.RequirePermission(middleware.PermServiceRead), adminH.ListServiceVersions)
	g.POST("/services/:id/versions", middleware.RequirePermission(middleware.PermServiceWrite), adminH.CreateServiceVersion)
	g.POST("/services/:id/publish", middleware.RequirePermission(middleware.PermServiceWrite), adminH.PublishServiceVersion)
	g.POST("/services/:id/promote", middleware.RequirePermission(middleware.PermServiceWrite), adminH.PromoteStagedServiceVersion)
	g.POST("/services/:id/rollback", middleware.RequirePermission(middleware.PermServiceWrite), adminH.RollbackServiceVersion)
	g.GET("/services/:id/subscriptions", middleware.RequirePermission(middleware.PermServiceRead), adminH.ListServiceSubscriptions)
	g.POST("/services/:id/subscriptions", middleware.RequirePermission(middleware.PermServiceWrite), adminH.CreateServiceSubscription)
	g.GET("/subscriptions/:id", middleware.RequirePermission(middleware.PermServiceRead), adminH.GetServiceSubscription)
	g.POST("/subscriptions/:id/review", middleware.RequirePermission(middleware.PermServiceWrite), adminH.ReviewServiceSubscription)
	g.GET("/keys", middleware.RequirePermission(middleware.PermAPIKeyRead), adminH.ListAPIKeys)
	g.POST("/keys", middleware.RequirePermission(middleware.PermAPIKeyWrite), adminH.CreateAPIKey)
	g.GET("/keys/:id", middleware.RequirePermission(middleware.PermAPIKeyRead), adminH.GetAPIKey)
	g.PUT("/keys/:id", middleware.RequirePermission(middleware.PermAPIKeyWrite), adminH.UpdateAPIKey)
	g.POST("/keys/:id/rotate", middleware.RequirePermission(middleware.PermAPIKeyWrite), adminH.RotateAPIKey)
	g.POST("/keys/:id/revoke", middleware.RequirePermission(middleware.PermAPIKeyWrite), adminH.RevokeAPIKey)
	g.GET("/virtual-keys", middleware.RequirePermission(middleware.PermVirtualKeyRead), adminH.ListVirtualKeys)
	g.POST("/virtual-keys", middleware.RequirePermission(middleware.PermVirtualKeyWrite), adminH.CreateVirtualKey)
	g.GET("/virtual-keys/:id", middleware.RequirePermission(middleware.PermVirtualKeyRead), adminH.GetVirtualKey)
	g.PUT("/virtual-keys/:id", middleware.RequirePermission(middleware.PermVirtualKeyWrite), adminH.UpdateVirtualKey)
	g.DELETE("/virtual-keys/:id", middleware.RequirePermission(middleware.PermVirtualKeyWrite), adminH.DeleteVirtualKey)
	g.GET("/users", middleware.RequirePermission(middleware.PermUserRead), adminH.ListUsers)
	g.POST("/users", middleware.RequirePermission(middleware.PermUserWrite), adminH.CreateUser)
	g.GET("/users/:id", middleware.RequirePermission(middleware.PermUserRead), adminH.GetUser)
	g.PUT("/users/:id", middleware.RequirePermission(middleware.PermUserWrite), adminH.UpdateUser)
	g.DELETE("/users/:id", middleware.RequirePermission(middleware.PermUserWrite), adminH.DeleteUser)
	g.POST("/users/:id/reset", middleware.RequirePermission(middleware.PermUserWrite), adminH.ResetUserUsage)
	g.GET("/users/:id/usage", middleware.RequirePermission(middleware.PermUsageRead), adminH.GetUserUsage)
	g.GET("/projects", middleware.RequirePermission(middleware.PermProjectRead), adminH.ListProjects)
	g.POST("/projects", middleware.RequirePermission(middleware.PermProjectWrite), adminH.CreateProject)
	g.GET("/projects/:id", middleware.RequirePermission(middleware.PermProjectRead), adminH.GetProject)
	g.GET("/projects/:id/usage", middleware.RequirePermission(middleware.PermUsageRead), adminH.GetProjectUsage)
	g.PUT("/projects/:id", middleware.RequirePermission(middleware.PermProjectWrite), adminH.UpdateProject)
	g.DELETE("/projects/:id", middleware.RequirePermission(middleware.PermProjectWrite), adminH.DeleteProject)
	g.GET("/responses", middleware.RequirePermission(middleware.PermResponseRead), adminH.ListResponses)
	g.GET("/responses/:id/trace", middleware.RequirePermission(middleware.PermResponseRead), adminH.GetResponseTrace)
	g.GET("/budgets", middleware.RequirePermission(middleware.PermBudgetRead), adminH.GetBudgets)
	g.GET("/usage/summary", middleware.RequirePermission(middleware.PermUsageRead), adminH.GetUsageSummary)
	g.GET("/usage/breakdown", middleware.RequirePermission(middleware.PermUsageRead), adminH.GetUsageBreakdown)
	g.GET("/usage/trend", middleware.RequirePermission(middleware.PermUsageRead), adminH.GetUsageTrend)
	g.POST("/reload", middleware.RequirePermission(middleware.PermConfigWrite), adminH.ReloadConfig)
}

func registerTenantRoutes(g *gin.RouterGroup, adminH *AdminHandler) {
	g.GET("", middleware.RequirePermission(middleware.PermTenantRead), adminH.ListTenants)
	g.POST("", middleware.RequirePermission(middleware.PermTenantWrite), adminH.CreateTenant)
	g.GET("/:id", middleware.RequirePermission(middleware.PermTenantRead), adminH.GetTenant)
	g.PUT("/:id", middleware.RequirePermission(middleware.PermTenantWrite), adminH.UpdateTenant)
	g.DELETE("/:id", middleware.RequirePermission(middleware.PermTenantWrite), adminH.DeleteTenant)
	g.POST("/:id/providers", middleware.RequirePermission(middleware.PermTenantWrite), adminH.ReplaceTenantProviders)
}

func (s *Server) Start() error {
	s.srv = s.buildHTTPServer()
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
