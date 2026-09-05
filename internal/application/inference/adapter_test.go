package inference_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gateyes/gateway/internal/application/inference"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

type recordingExecutor struct {
	createResult *responseSvc.CreateResult
	createErr    error
	streamResult *responseSvc.Stream
	streamErr    error
	states       map[string]int
	persisted    bool
}

func (e *recordingExecutor) Create(context.Context, *repository.AuthIdentity, *provider.ResponseRequest, string) (*responseSvc.CreateResult, error) {
	return e.createResult, e.createErr
}

func (e *recordingExecutor) CreateStream(context.Context, *repository.AuthIdentity, *provider.ResponseRequest, string) (*responseSvc.Stream, error) {
	return e.streamResult, e.streamErr
}

func (e *recordingExecutor) GetCircuitBreakerStates() map[string]int { return e.states }

func (e *recordingExecutor) PersistCircuitBreakerState(context.Context) { e.persisted = true }

func TestOrchestratedAdapterDelegatesCreate(t *testing.T) {
	expectedCreate := &responseSvc.CreateResult{ProviderName: "primary", Retries: 2, Fallback: 1}
	executor := &recordingExecutor{createResult: expectedCreate}
	adapter := inference.NewOrchestrated(inference.Dependencies{Executor: executor})

	created, err := adapter.Create(context.Background(), &repository.AuthIdentity{}, &provider.ResponseRequest{}, "session")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created != expectedCreate {
		t.Fatalf("Create() result = %#v, want original production result %#v", created, expectedCreate)
	}
}

func TestOrchestratedAdapterDelegatesCreateStream(t *testing.T) {
	expectedStream := &responseSvc.Stream{ResponseID: "response-id", ProviderName: "fallback"}
	executor := &recordingExecutor{streamResult: expectedStream}
	adapter := inference.NewOrchestrated(inference.Dependencies{Executor: executor})

	streamed, err := adapter.CreateStream(context.Background(), &repository.AuthIdentity{}, &provider.ResponseRequest{}, "session")
	if err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	if streamed != expectedStream {
		t.Fatalf("CreateStream() result = %#v, want original production stream %#v", streamed, expectedStream)
	}
}

func TestOrchestratedAdapterPreservesExecutorErrors(t *testing.T) {
	wantErr := errors.New("production execution failed")
	adapter := inference.NewOrchestrated(inference.Dependencies{Executor: &recordingExecutor{createErr: wantErr}})

	_, err := adapter.Create(context.Background(), &repository.AuthIdentity{}, &provider.ResponseRequest{}, "session")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Create() error = %v, want %v", err, wantErr)
	}
}

func TestOrchestratedAdapterRejectsMissingProductionExecutor(t *testing.T) {
	adapter := inference.NewOrchestrated(inference.Dependencies{})

	_, err := adapter.Create(context.Background(), &repository.AuthIdentity{}, &provider.ResponseRequest{}, "session")
	if !errors.Is(err, inference.ErrProductionExecutorRequired) {
		t.Fatalf("Create() error = %v, want %v", err, inference.ErrProductionExecutorRequired)
	}
}

func TestOrchestratedAdapterDelegatesCircuitBreakerStates(t *testing.T) {
	executor := &recordingExecutor{states: map[string]int{"tenant:provider": 2}}
	adapter := inference.NewOrchestrated(inference.Dependencies{Executor: executor})

	if got := adapter.GetCircuitBreakerStates(); !reflect.DeepEqual(got, executor.states) {
		t.Fatalf("GetCircuitBreakerStates() = %#v, want %#v", got, executor.states)
	}
}

func TestOrchestratedAdapterDelegatesCircuitBreakerPersistence(t *testing.T) {
	executor := &recordingExecutor{}
	adapter := inference.NewOrchestrated(inference.Dependencies{Executor: executor})

	adapter.PersistCircuitBreakerState(context.Background())
	if !executor.persisted {
		t.Fatal("PersistCircuitBreakerState() did not reach production executor")
	}
}
