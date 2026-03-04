package server

import (
	"net/http"

	"gateyes/internal/config"
	"gateyes/internal/handler"
	"gateyes/internal/middleware"
)

type Handlers struct {
	Health  *handler.HealthHandler
	Gateway *handler.GatewayHandlers
	Admin   *handler.AdminHandlers
}

func New(cfg config.Config, handlers Handlers) *http.Server {
	rootMux := http.NewServeMux()
	rootMux.HandleFunc("GET /healthz", handlers.Health.Handle)

	gatewayMux := http.NewServeMux()
	gatewayMux.HandleFunc("GET /v1/models", handlers.Gateway.Models)
	gatewayMux.HandleFunc("POST /v1/chat/completions", handlers.Gateway.ChatCompletions)
	gatewayMux.HandleFunc("POST /v1/embeddings", handlers.Gateway.Embeddings)

	gatewayHandler := middleware.Chain(
		gatewayMux,
		middleware.RecoverJSON(),
		middleware.RequestContext(),
		middleware.GatewayAuth(cfg.Auth.EnableGatewayAuth),
	)
	rootMux.Handle("/v1/", gatewayHandler)

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("POST /api/v1/admin/channels", handlers.Admin.CreateChannel)

	adminHandler := middleware.Chain(
		adminMux,
		middleware.RecoverJSON(),
		middleware.RequestContext(),
		middleware.AdminAuth(cfg.Auth.AdminToken),
	)
	rootMux.Handle("/api/v1/admin/", adminHandler)

	// TODO(io): add metrics, structured logging, and tracing middleware here.
	return &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           rootMux,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}
}
