// Package inference contains application adapters for inference use cases.
package inference

import (
	"context"
	"time"

	"github.com/gateyes/gateway/internal/ports"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

// Adapter exposes the existing response service through the application port.
type Adapter struct{ service *responseSvc.Service }

func New(service *responseSvc.Service) *Adapter { return &Adapter{service: service} }

// OrchestratedAdapter exposes the port-driven workflow through the existing
// transport-facing InferenceUseCase contract. It lets composition roots opt
// into the new application orchestration without changing HTTP handlers.
type OrchestratedAdapter struct{ orchestrator *Orchestrator }

func NewOrchestrated(deps Dependencies) *OrchestratedAdapter {
	return &OrchestratedAdapter{orchestrator: NewOrchestrator(deps)}
}

func (a *OrchestratedAdapter) Create(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*responseSvc.CreateResult, error) {
	result, err := a.orchestrator.Create(ctx, identity, req, sessionID)
	if err != nil {
		return nil, err
	}
	return &responseSvc.CreateResult{
		Response:         result.Response,
		ProviderName:     result.ProviderName,
		LatencyMs:        result.Latency.Milliseconds(),
		PromptTokens:     result.Response.Usage.PromptTokens,
		CompletionTokens: result.Response.Usage.CompletionTokens,
		Retries:          result.Retries,
		Fallback:         result.Fallback,
	}, nil
}

func (a *OrchestratedAdapter) CreateStream(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, sessionID string) (*responseSvc.Stream, error) {
	result, err := a.orchestrator.CreateStream(ctx, identity, req, sessionID)
	if err != nil {
		return nil, err
	}
	return &responseSvc.Stream{ResponseID: result.ResponseID, ProviderName: result.ProviderName, StartedAt: time.Now(), Events: result.Events, Errors: result.Errors}, nil
}

func (a *OrchestratedAdapter) GetCircuitBreakerStates() map[string]int    { return map[string]int{} }
func (a *OrchestratedAdapter) PersistCircuitBreakerState(context.Context) {}

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
