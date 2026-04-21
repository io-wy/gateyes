package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
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
	engine.Use(gin.Recovery())
	engine.Use(middleware.Correlation())
	engine.Use(gin.Logger())

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
	}

	serviceRoutes := engine.Group("/service/:prefix")
	serviceRoutes.Use(mw.Auth())
	{
		serviceRoutes.POST("/responses", h.ServiceResponses)
		serviceRoutes.POST("/chat/completions", h.ServiceChat)
		serviceRoutes.POST("/messages", h.ServiceMessages)
		serviceRoutes.POST("/invoke", h.ServiceInvoke)
	}

	admin := engine.Group("/admin")
	admin.Use(mw.Auth())
	admin.Use(mw.RequireRoles(repository.RoleTenantAdmin, repository.RoleSuperAdmin))
	{
		admin.GET("/dashboard", adminH.Dashboard)
		admin.GET("/providers", adminH.GetProviders)
		admin.GET("/providers/:name", adminH.GetProvider)
		admin.GET("/providers/:name/stats", adminH.GetProviderStats)
		admin.PUT("/providers/:name", adminH.UpdateProvider)
		admin.GET("/audit", adminH.ListAuditLogs)
		admin.GET("/services", adminH.ListServices)
		admin.POST("/services", adminH.CreateService)
		admin.GET("/services/:id", adminH.GetService)
		admin.PUT("/services/:id", adminH.UpdateService)
		admin.GET("/services/:id/versions", adminH.ListServiceVersions)
		admin.POST("/services/:id/versions", adminH.CreateServiceVersion)
		admin.POST("/services/:id/publish", adminH.PublishServiceVersion)
		admin.POST("/services/:id/promote", adminH.PromoteStagedServiceVersion)
		admin.POST("/services/:id/rollback", adminH.RollbackServiceVersion)
		admin.GET("/services/:id/subscriptions", adminH.ListServiceSubscriptions)
		admin.POST("/services/:id/subscriptions", adminH.CreateServiceSubscription)
		admin.GET("/subscriptions/:id", adminH.GetServiceSubscription)
		admin.POST("/subscriptions/:id/review", adminH.ReviewServiceSubscription)
		admin.GET("/keys", adminH.ListAPIKeys)
		admin.POST("/keys", adminH.CreateAPIKey)
		admin.GET("/keys/:id", adminH.GetAPIKey)
		admin.PUT("/keys/:id", adminH.UpdateAPIKey)
		admin.POST("/keys/:id/rotate", adminH.RotateAPIKey)
		admin.POST("/keys/:id/revoke", adminH.RevokeAPIKey)
		admin.GET("/users", adminH.ListUsers)
		admin.POST("/users", adminH.CreateUser)
		admin.GET("/users/:id", adminH.GetUser)
		admin.PUT("/users/:id", adminH.UpdateUser)
		admin.DELETE("/users/:id", adminH.DeleteUser)
		admin.POST("/users/:id/reset", adminH.ResetUserUsage)
		admin.GET("/users/:id/usage", adminH.GetUserUsage)
		admin.GET("/projects", adminH.ListProjects)
		admin.POST("/projects", adminH.CreateProject)
		admin.GET("/projects/:id", adminH.GetProject)
		admin.GET("/projects/:id/usage", adminH.GetProjectUsage)
		admin.PUT("/projects/:id", adminH.UpdateProject)
		admin.GET("/responses/:id/trace", adminH.GetResponseTrace)
		admin.GET("/usage/summary", adminH.GetUsageSummary)
		admin.GET("/usage/breakdown", adminH.GetUsageBreakdown)
		admin.GET("/usage/trend", adminH.GetUsageTrend)
	}

	tenants := admin.Group("/tenants")
	tenants.Use(mw.RequireRoles(repository.RoleSuperAdmin))
	{
		tenants.GET("", adminH.ListTenants)
		tenants.POST("", adminH.CreateTenant)
		tenants.GET("/:id", adminH.GetTenant)
		tenants.PUT("/:id", adminH.UpdateTenant)
		tenants.POST("/:id/providers", adminH.ReplaceTenantProviders)
	}

	return &Server{cfg: cfg, engine: engine}
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
