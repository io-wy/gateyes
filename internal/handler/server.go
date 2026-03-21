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
	engine.Use(gin.Logger())

	engine.GET("/debug/pprof/*path", gin.WrapF(http.DefaultServeMux.ServeHTTP))
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
	}

	admin := engine.Group("/admin")
	admin.Use(mw.Auth())
	admin.Use(mw.RequireRoles(repository.RoleTenantAdmin, repository.RoleSuperAdmin))
	{
		admin.GET("/dashboard", adminH.Dashboard)
		admin.GET("/providers", adminH.GetProviders)
		admin.GET("/providers/:name", adminH.GetProvider)
		admin.GET("/providers/:name/stats", adminH.GetProviderStats)
		admin.GET("/users", adminH.ListUsers)
		admin.POST("/users", adminH.CreateUser)
		admin.GET("/users/:id", adminH.GetUser)
		admin.PUT("/users/:id", adminH.UpdateUser)
		admin.DELETE("/users/:id", adminH.DeleteUser)
		admin.POST("/users/:id/reset", adminH.ResetUserUsage)
		admin.GET("/users/:id/usage", adminH.GetUserUsage)
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
	s.srv = &http.Server{
		Addr:         s.cfg.ListenAddr,
		Handler:      s.engine,
		ReadTimeout:  time.Duration(s.cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.cfg.WriteTimeout) * time.Second,
	}
	return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return fmt.Errorf("server not started")
	}
	return s.srv.Shutdown(ctx)
}
