package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/app/config"
	mw "github.com/gateyes/gateway/internal/transport/http/middleware"
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
	adminV1.Use(mwSvc.AdminAuth())
	registerAdminRoutes(adminV1, adminH)

	// Admin legacy compatibility alias (/admin/* → /admin/v1/*)
	adminLegacy := engine.Group("/admin")
	adminLegacy.Use(mwSvc.AdminAuth())
	registerAdminRoutes(adminLegacy, adminH)

	// Tenant routes under v1
	tenantsV1 := adminV1.Group("/tenants")
	tenantsV1.Use(mw.RequirePermission(mw.PermTenantWrite))
	registerTenantRoutes(tenantsV1, adminH)

	// Tenant routes under legacy
	tenantsLegacy := adminLegacy.Group("/tenants")
	tenantsLegacy.Use(mw.RequirePermission(mw.PermTenantWrite))
	registerTenantRoutes(tenantsLegacy, adminH)

	s := &Server{cfg: cfg, engine: engine}
	return s
}

func registerAdminRoutes(g *gin.RouterGroup, adminH *AdminHandler) {
	g.GET("/dashboard", mw.RequirePermission(mw.PermUsageRead), adminH.Dashboard)
	g.GET("/providers", mw.RequirePermission(mw.PermProviderRead), adminH.GetProviders)
	g.POST("/providers/check", mw.RequirePermission(mw.PermProviderRead), adminH.CheckProviders)
	g.POST("/providers", mw.RequirePermission(mw.PermProviderWrite), adminH.CreateProvider)
	g.GET("/providers/:name", mw.RequirePermission(mw.PermProviderRead), adminH.GetProvider)
	g.GET("/providers/:name/stats", mw.RequirePermission(mw.PermProviderRead), adminH.GetProviderStats)
	g.PUT("/providers/:name", mw.RequirePermission(mw.PermProviderWrite), adminH.UpdateProvider)
	g.DELETE("/providers/:name", mw.RequirePermission(mw.PermProviderWrite), adminH.DeleteProvider)
	g.GET("/audit", mw.RequirePermission(mw.PermAuditRead), adminH.ListAuditLogs)
	g.GET("/services", mw.RequirePermission(mw.PermServiceRead), adminH.ListServices)
	g.POST("/services", mw.RequirePermission(mw.PermServiceWrite), adminH.CreateService)
	g.GET("/services/:id", mw.RequirePermission(mw.PermServiceRead), adminH.GetService)
	g.PUT("/services/:id", mw.RequirePermission(mw.PermServiceWrite), adminH.UpdateService)
	g.GET("/services/:id/versions", mw.RequirePermission(mw.PermServiceRead), adminH.ListServiceVersions)
	g.POST("/services/:id/versions", mw.RequirePermission(mw.PermServiceWrite), adminH.CreateServiceVersion)
	g.POST("/services/:id/publish", mw.RequirePermission(mw.PermServiceWrite), adminH.PublishServiceVersion)
	g.POST("/services/:id/promote", mw.RequirePermission(mw.PermServiceWrite), adminH.PromoteStagedServiceVersion)
	g.POST("/services/:id/rollback", mw.RequirePermission(mw.PermServiceWrite), adminH.RollbackServiceVersion)
	g.GET("/services/:id/subscriptions", mw.RequirePermission(mw.PermServiceRead), adminH.ListServiceSubscriptions)
	g.POST("/services/:id/subscriptions", mw.RequirePermission(mw.PermServiceWrite), adminH.CreateServiceSubscription)
	g.GET("/subscriptions/:id", mw.RequirePermission(mw.PermServiceRead), adminH.GetServiceSubscription)
	g.POST("/subscriptions/:id/review", mw.RequirePermission(mw.PermServiceWrite), adminH.ReviewServiceSubscription)
	g.GET("/keys", mw.RequirePermission(mw.PermAPIKeyRead), adminH.ListAPIKeys)
	g.POST("/keys", mw.RequirePermission(mw.PermAPIKeyWrite), adminH.CreateAPIKey)
	g.GET("/keys/:id", mw.RequirePermission(mw.PermAPIKeyRead), adminH.GetAPIKey)
	g.PUT("/keys/:id", mw.RequirePermission(mw.PermAPIKeyWrite), adminH.UpdateAPIKey)
	g.POST("/keys/:id/rotate", mw.RequirePermission(mw.PermAPIKeyWrite), adminH.RotateAPIKey)
	g.POST("/keys/:id/revoke", mw.RequirePermission(mw.PermAPIKeyWrite), adminH.RevokeAPIKey)
	g.GET("/virtual-keys", mw.RequirePermission(mw.PermVirtualKeyRead), adminH.ListVirtualKeys)
	g.POST("/virtual-keys", mw.RequirePermission(mw.PermVirtualKeyWrite), adminH.CreateVirtualKey)
	g.GET("/virtual-keys/:id", mw.RequirePermission(mw.PermVirtualKeyRead), adminH.GetVirtualKey)
	g.PUT("/virtual-keys/:id", mw.RequirePermission(mw.PermVirtualKeyWrite), adminH.UpdateVirtualKey)
	g.DELETE("/virtual-keys/:id", mw.RequirePermission(mw.PermVirtualKeyWrite), adminH.DeleteVirtualKey)
	g.GET("/users", mw.RequirePermission(mw.PermUserRead), adminH.ListUsers)
	g.POST("/users", mw.RequirePermission(mw.PermUserWrite), adminH.CreateUser)
	g.GET("/users/:id", mw.RequirePermission(mw.PermUserRead), adminH.GetUser)
	g.PUT("/users/:id", mw.RequirePermission(mw.PermUserWrite), adminH.UpdateUser)
	g.DELETE("/users/:id", mw.RequirePermission(mw.PermUserWrite), adminH.DeleteUser)
	g.POST("/users/:id/reset", mw.RequirePermission(mw.PermUserWrite), adminH.ResetUserUsage)
	g.GET("/users/:id/usage", mw.RequirePermission(mw.PermUsageRead), adminH.GetUserUsage)
	g.GET("/projects", mw.RequirePermission(mw.PermProjectRead), adminH.ListProjects)
	g.POST("/projects", mw.RequirePermission(mw.PermProjectWrite), adminH.CreateProject)
	g.GET("/projects/:id", mw.RequirePermission(mw.PermProjectRead), adminH.GetProject)
	g.GET("/projects/:id/usage", mw.RequirePermission(mw.PermUsageRead), adminH.GetProjectUsage)
	g.PUT("/projects/:id", mw.RequirePermission(mw.PermProjectWrite), adminH.UpdateProject)
	g.DELETE("/projects/:id", mw.RequirePermission(mw.PermProjectWrite), adminH.DeleteProject)
	g.GET("/responses", mw.RequirePermission(mw.PermResponseRead), adminH.ListResponses)
	g.GET("/responses/:id/trace", mw.RequirePermission(mw.PermResponseRead), adminH.GetResponseTrace)
	g.GET("/budgets", mw.RequirePermission(mw.PermBudgetRead), adminH.GetBudgets)
	g.GET("/usage/summary", mw.RequirePermission(mw.PermUsageRead), adminH.GetUsageSummary)
	g.GET("/usage/breakdown", mw.RequirePermission(mw.PermUsageRead), adminH.GetUsageBreakdown)
	g.GET("/usage/trend", mw.RequirePermission(mw.PermUsageRead), adminH.GetUsageTrend)
	g.POST("/reload", mw.RequirePermission(mw.PermConfigWrite), adminH.ReloadConfig)
}

func registerTenantRoutes(g *gin.RouterGroup, adminH *AdminHandler) {
	g.GET("", mw.RequirePermission(mw.PermTenantRead), adminH.ListTenants)
	g.POST("", mw.RequirePermission(mw.PermTenantWrite), adminH.CreateTenant)
	g.GET("/:id", mw.RequirePermission(mw.PermTenantRead), adminH.GetTenant)
	g.PUT("/:id", mw.RequirePermission(mw.PermTenantWrite), adminH.UpdateTenant)
	g.DELETE("/:id", mw.RequirePermission(mw.PermTenantWrite), adminH.DeleteTenant)
	g.POST("/:id/providers", mw.RequirePermission(mw.PermTenantWrite), adminH.ReplaceTenantProviders)
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
