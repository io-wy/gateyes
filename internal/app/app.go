package app

import (
	"net/http"

	"gateyes/internal/concurrency"
	"gateyes/internal/config"
	"gateyes/internal/handler"
	"gateyes/internal/scheduler"
	"gateyes/internal/server"
)

type Application struct {
	Server *http.Server
}

func New(cfg config.Config, buildInfo handler.BuildInfo) (*Application, error) {
	selector := scheduler.NewStaticSelector(
		cfg.Scheduler.DefaultChannelID,
		cfg.Scheduler.DefaultProvider,
		cfg.Scheduler.DefaultUpstreamModel,
	)

	limiter := concurrency.NewMemoryManager(concurrency.Limits{
		Global:     cfg.Concurrency.GlobalLimit,
		PerChannel: cfg.Concurrency.DefaultChannelLimit,
		PerToken:   cfg.Concurrency.DefaultTokenLimit,
	})

	healthHandler := handler.NewHealthHandler(buildInfo)
	gatewayHandler := handler.NewGatewayHandlers(
		selector,
		limiter,
		[]string{cfg.Scheduler.DefaultUpstreamModel},
	)
	adminHandler := handler.NewAdminHandlers()

	httpServer := server.New(cfg, server.Handlers{
		Health:  healthHandler,
		Gateway: gatewayHandler,
		Admin:   adminHandler,
	})

	return &Application{Server: httpServer}, nil
}

func (a *Application) Cleanup() {
	// TODO(io): close DB / Redis / metrics exporters once those dependencies are added.
}
