// Package ports contains the application-facing contracts used by transport
// handlers. Concrete persistence implementations stay behind these ports.
package ports

import (
	"context"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/adminconsole"
	"github.com/gateyes/gateway/internal/service/catalog"
)

type PingPort interface {
	Ping(context.Context) error
}

// AdminAccessPort is the persistence surface used by administration use cases.
// It is intentionally composed from repository capabilities instead of
// exposing a concrete sqlstore implementation to handlers.
type AdminAccessPort interface {
	repository.UserStore
	repository.APIKeyStore
	repository.IdentityStore
	repository.UsageStore
	repository.TenantStore
	repository.ResponseStore
	repository.ProviderRegistryStore
	repository.ProjectStore
	repository.ServiceStore
	repository.PluginStore
	repository.AuditLogStore
	repository.VirtualKeyStore
	repository.RoleStore
	repository.SemanticCacheStore
	PingPort
}

// AccessPort is the short alias used by application wiring.
type AccessPort = AdminAccessPort

type AuthIdentity = repository.AuthIdentity
type UserRecord = repository.UserRecord
type APIKeyRecord = repository.APIKeyRecord
type VirtualKeyRecord = repository.VirtualKeyRecord
type TenantRecord = repository.TenantRecord
type ProjectRecord = repository.ProjectRecord
type PluginRecord = repository.PluginRecord
type RoleRecord = repository.RoleRecord
type ProviderRegistryRecord = repository.ProviderRegistryRecord

const (
	StatusActive    = repository.StatusActive
	StatusInactive  = repository.StatusInactive
	StatusRevoked   = repository.StatusRevoked
	RoleSuperAdmin  = repository.RoleSuperAdmin
	RoleTenantAdmin = repository.RoleTenantAdmin
	RoleTenantUser  = repository.RoleTenantUser
)

var ErrNotFound = repository.ErrNotFound

// AdminConsoleUseCase is the application surface consumed by admin HTTP
// handlers. adminconsole.Service is the current in-process adapter.
type AdminConsoleUseCase interface {
	Me(context.Context, *repository.AuthIdentity) (*adminconsole.IdentityView, error)
	CreateAPIKey(context.Context, *repository.AuthIdentity, adminconsole.CreateAPIKeyInput) (*adminconsole.APIKeyWithSecret, error)
	ListAPIKeys(context.Context, *repository.AuthIdentity, repository.APIKeyFilter) ([]repository.APIKeyRecord, error)
	GetAPIKey(context.Context, *repository.AuthIdentity, string) (*repository.APIKeyRecord, error)
	UpdateAPIKey(context.Context, *repository.AuthIdentity, string, repository.UpdateAPIKeyParams) (*repository.APIKeyRecord, error)
	RotateAPIKey(context.Context, *repository.AuthIdentity, string) (*adminconsole.APIKeyWithSecret, error)
	RevokeAPIKey(context.Context, *repository.AuthIdentity, string) (*repository.APIKeyRecord, error)
	ListVirtualKeys(context.Context, *repository.AuthIdentity, repository.VirtualKeyFilter) ([]repository.VirtualKeyRecord, error)
	GetVirtualKey(context.Context, *repository.AuthIdentity, string) (*repository.VirtualKeyRecord, error)
	CreateVirtualKey(context.Context, *repository.AuthIdentity, adminconsole.CreateVirtualKeyInput) (*adminconsole.VirtualKeyWithSecret, error)
	UpdateVirtualKey(context.Context, *repository.AuthIdentity, string, repository.UpdateVirtualKeyParams) (*repository.VirtualKeyRecord, error)
	DeleteVirtualKey(context.Context, *repository.AuthIdentity, string) error
	ListResponses(context.Context, *repository.AuthIdentity, repository.ResponseFilter) ([]repository.ResponseRecord, int, error)
	GetResponse(context.Context, *repository.AuthIdentity, string) (*repository.ResponseRecord, error)
	Dashboard(context.Context, *repository.AuthIdentity) (*adminconsole.DashboardSummary, error)
	UsageSummary(context.Context, *repository.AuthIdentity, repository.UsageFilter) (*repository.UsageStats, repository.UsageFilter, error)
	UsageBreakdown(context.Context, *repository.AuthIdentity, repository.UsageFilter, string) ([]repository.UsageBreakdownRow, repository.UsageFilter, error)
	UsageTrend(context.Context, *repository.AuthIdentity, repository.UsageFilter, string, int) ([]repository.UsageTimeBucket, repository.UsageFilter, error)
	ListServices(context.Context, *repository.AuthIdentity, repository.ServiceFilter) ([]repository.ServiceRecord, error)
	GetService(context.Context, *repository.AuthIdentity, string) (*catalog.ServiceWithVersions, error)
	Catalog(context.Context, *repository.AuthIdentity, string, string) (*adminconsole.CatalogView, error)
}

// AdminAccessUseCase is kept as the domain-oriented name for the console
// contract; both names describe the same application boundary.
type AdminAccessUseCase = AdminConsoleUseCase
