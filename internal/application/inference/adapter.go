// Package inference contains application adapters for inference use cases.
package inference

import (
	"context"

	"github.com/gateyes/gateway/internal/ports"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

// ProductionExecutor is the complete inference execution contract retained
// during the incremental application-layer migration. Keeping it as a port
// preserves production semantics without coupling the adapter to a concrete
// response service.
type ProductionExecutor interface {
	ports.InferenceUseCase
}

// Adapter exposes the existing response service through the application port.
type Adapter struct{ service ProductionExecutor }

func New(service ProductionExecutor) *Adapter { return &Adapter{service: service} }

// OrchestratedAdapter exposes the port-driven workflow through the existing
// transport-facing InferenceUseCase contract. It lets composition roots opt
// into the new application orchestration without changing HTTP handlers.
type OrchestratedAdapter struct {
	orchestrator *Orchestrator
}

func NewOrchestrated(deps Dependencies) *OrchestratedAdapter {
	return &OrchestratedAdapter{orchestrator: NewOrchestrator(deps)}
}

func (a *OrchestratedAdapter) Create(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*responseSvc.CreateResult, error) {
	return a.orchestrator.Execute(ctx, identity, req, sessionID)
}

func (a *OrchestratedAdapter) CreateStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*responseSvc.Stream, error) {
	return a.orchestrator.ExecuteStream(ctx, identity, req, sessionID)
}

func (a *OrchestratedAdapter) GetCircuitBreakerStates() map[string]int {
	return a.orchestrator.GetCircuitBreakerStates()
}

func (a *OrchestratedAdapter) PersistCircuitBreakerState(ctx context.Context) {
	a.orchestrator.PersistCircuitBreakerState(ctx)
}

var _ ports.InferenceUseCase = (*OrchestratedAdapter)(nil)

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
