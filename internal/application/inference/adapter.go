// Package inference contains application adapters for inference use cases.
package inference

import (
	"context"

	"github.com/gateyes/gateway/internal/ports"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

// Adapter exposes the existing response service through the application port.
type Adapter struct{ service *responseSvc.Service }

func New(service *responseSvc.Service) *Adapter { return &Adapter{service: service} }

func (a *Adapter) Create(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*responseSvc.CreateResult, error) {
	return a.service.Create(ctx, identity, req, sessionID)
}

func (a *Adapter) CreateStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*responseSvc.Stream, error) {
	return a.service.CreateStream(ctx, identity, req, sessionID)
}

func (a *Adapter) GetCircuitBreakerStates() map[string]int {
	return a.service.GetCircuitBreakerStates()
}

func (a *Adapter) PersistCircuitBreakerState(ctx context.Context) {
	a.service.PersistCircuitBreakerState(ctx)
}

var _ ports.InferenceUseCase = (*Adapter)(nil)
