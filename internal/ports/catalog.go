package ports

import (
	"context"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

// CatalogUseCase covers both service-runtime inference and administration
// catalog operations. The concrete catalog service is an adapter.
type CatalogUseCase interface {
	Create(context.Context, *repository.AuthIdentity, string, string, *provider.ResponseRequest, string) (*responseSvc.CreateResult, *repository.ServiceRecord, error)
	CreateStream(context.Context, *repository.AuthIdentity, string, string, *provider.ResponseRequest, string) (*responseSvc.Stream, *repository.ServiceRecord, error)
	CreatePromptInvocation(context.Context, *repository.AuthIdentity, string, catalog.PromptInvokeRequest, string) (*responseSvc.CreateResult, *repository.ServiceRecord, error)
	CreatePromptInvocationStream(context.Context, *repository.AuthIdentity, string, catalog.PromptInvokeRequest, string) (*responseSvc.Stream, *repository.ServiceRecord, error)
	CreateServiceWithOptions(context.Context, repository.CreateServiceParams, catalog.CreateServiceOptions) (*catalog.CreateServiceResult, error)
	UpdateService(context.Context, string, string, repository.UpdateServiceParams) (*repository.ServiceRecord, error)
	DeleteService(context.Context, string, string) error
	ListServiceVersions(context.Context, string, string) ([]repository.ServiceVersionRecord, error)
	CreateServiceVersion(context.Context, string, string) (*repository.ServiceVersionRecord, error)
	PublishServiceVersion(context.Context, string, string, string, string) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error)
	PromoteStagedServiceVersion(context.Context, string, string) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error)
	RollbackServiceVersion(context.Context, string, string, string) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error)
	CreateSubscription(context.Context, string, string, repository.CreateServiceSubscriptionParams) (*repository.ServiceSubscriptionRecord, error)
	ListSubscriptions(context.Context, string, string, repository.ServiceSubscriptionFilter) ([]repository.ServiceSubscriptionRecord, error)
	GetSubscription(context.Context, string, string) (*repository.ServiceSubscriptionRecord, error)
	ReviewSubscription(context.Context, string, string, string, string) (*catalog.ReviewSubscriptionResult, error)
}

type CatalogPort = CatalogUseCase

// ProviderView is re-exported for adapters that expose catalog snapshots.
type ProviderView = provider.ProviderView
