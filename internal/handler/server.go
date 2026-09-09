package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/app/config"
	mw "github.com/gateyes/gateway/internal/handler/middleware"
	admintransport "github.com/gateyes/gateway/internal/transport/http/admin"
	publictransport "github.com/gateyes/gateway/internal/transport/http/public"
)

var ErrServerClosed = fmt.Errorf("server closed")

type Server struct {
	cfg    config.ServerConfig
	engine *gin.Engine
	srv    *http.Server
}

// NewServer wires process middleware once, then delegates route ownership to
// the public and administration bounded-context transports.
func NewServer(cfg config.ServerConfig, h *Handler, adminH *AdminHandler, mwSvc *mw.Middleware) *Server {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(mw.Correlation())
	engine.Use(mw.OtelTrace())
	engine.Use(gin.Logger())
	engine.Use(gin.Recovery())

	publictransport.RegisterRoutes(engine, h, mwSvc)
	admintransport.RegisterRoutes(engine, adminH, mwSvc)

	return &Server{cfg: cfg, engine: engine}
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
