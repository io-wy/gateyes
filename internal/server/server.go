package server

import (
	"context"
	"net/http"

	"gateyes/internal/config"
)

var ErrServerClosed = http.ErrServerClosed

type Server struct {
	cfg        *config.Config
	httpServer *http.Server
}

func New(cfg *config.Config, handler http.Handler) *Server {
	return &Server{
		cfg: cfg,
		httpServer: &http.Server{
			Addr:         cfg.Server.ListenAddr,
			Handler:      handler,
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
