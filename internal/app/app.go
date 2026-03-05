package app

import (
	"net/http"

	"gateyes/internal/concurrency"
	"gateyes/internal/config"
	"gateyes/internal/scheduler"
	"gateyes/internal/service"
	"gateyes/internal/transport/http/handler"
	"gateyes/internal/transport/http/server"
)

type Application struct {
	Server *http.Server
}

func New(cfg config.Config, buildInfo service.BuildInfo) (*Application, error) {
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

	healthService := service.NewHealthService(buildInfo)
	gatewayService := service.NewGatewayService(
		selector,
		limiter,
		[]string{cfg.Scheduler.DefaultUpstreamModel},
	)

	healthHandler := handler.NewHealthHandler(healthService)
	gatewayHandler := handler.NewGatewayHandlers(gatewayService)

	httpServer := server.New(cfg, server.Handlers{
		Health:  healthHandler,
		Gateway: gatewayHandler,
	})

	return &Application{Server: httpServer}, nil
}

func (a *Application) Cleanup() {
	// TODO(io): close DB / Redis / metrics exporters once those dependencies are added.
}
