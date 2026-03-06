package server

import (
	"context"
	"net/http"

	"gateyes/internal/config"

	"github.com/gin-gonic/gin"
)

var ErrServerClosed = http.ErrServerClosed

type Server struct {
	cfg        *config.Config
	httpServer *http.Server
}

func New(cfg *config.Config, engine *gin.Engine) *Server {
	return &Server{
		cfg: cfg,
		httpServer: &http.Server{
			Addr:         cfg.Server.ListenAddr,
			Handler:      engine,
			ReadTimeout:  cfg.Server.ReadTimeout.Duration,
			WriteTimeout: cfg.Server.WriteTimeout.Duration,
			IdleTimeout:  cfg.Server.IdleTimeout.Duration,
		},
	}
}

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
