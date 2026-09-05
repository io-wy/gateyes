package ports

import (
	"context"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/batch"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

// InferenceUseCase is the minimum response execution surface needed by the
// public and service-runtime handlers.
type InferenceUseCase interface {
	Create(context.Context, *repository.AuthIdentity, *provider.ResponseRequest, string) (*responseSvc.CreateResult, error)
	CreateStream(context.Context, *repository.AuthIdentity, *provider.ResponseRequest, string) (*responseSvc.Stream, error)
	GetCircuitBreakerStates() map[string]int
	PersistCircuitBreakerState(context.Context)
}

type InferencePort = InferenceUseCase

// ModelQueryUseCase provides the read-only provider catalog used by /models
// and capability discovery endpoints.
type ModelQueryUseCase interface {
	ListTenantProviders(context.Context, string) ([]string, error)
	Ping(context.Context) error
}

// ProviderCatalog is the provider-manager surface needed by handlers.
type ProviderCatalog interface {
	Get(string) (provider.Provider, bool)
	List() []provider.Provider
	ListByNames([]string) []provider.Provider
	Registry(string) (repository.ProviderRegistryRecord, bool)
	ListRegistry() []repository.ProviderRegistryRecord
}

// BatchUseCase is implemented by the existing batch service adapter.
type BatchUseCase interface {
	Create(context.Context, *repository.AuthIdentity, batch.CreateRequest, []byte) (*repository.BatchJobRecord, error)
	Get(context.Context, string, string) (*repository.BatchJobRecord, error)
	List(context.Context, string, int, int) ([]repository.BatchJobRecord, error)
	Items(context.Context, string, string) ([]repository.BatchItemRecord, error)
	Cancel(context.Context, string, string) (*repository.BatchJobRecord, error)
}
