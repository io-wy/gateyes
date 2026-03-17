package repository

import (
	"context"
	"errors"
	"time"
)

const (
	StatusActive   = "active"
	StatusInactive = "inactive"

	RoleSuperAdmin  = "super_admin"
	RoleTenantAdmin = "tenant_admin"
	RoleTenantUser  = "tenant_user"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	UserStore
	IdentityStore
	UsageStore
	TenantStore
	ResponseStore
}

type UserStore interface {
	CreateUser(ctx context.Context, params CreateUserParams) (*UserRecord, error)
	ListUsers(ctx context.Context, tenantID string) ([]UserRecord, error)
	GetUser(ctx context.Context, tenantID string, idOrAPIKey string) (*UserRecord, error)
	UpdateUser(ctx context.Context, tenantID string, idOrAPIKey string, params UpdateUserParams) (*UserRecord, error)
	DeleteUser(ctx context.Context, tenantID string, idOrAPIKey string) error
	ResetUserUsage(ctx context.Context, tenantID string, idOrAPIKey string) (*UserRecord, error)
	Stats(ctx context.Context, tenantID string) (*UserStats, error)
}

type IdentityStore interface {
	Authenticate(ctx context.Context, key string) (*AuthIdentity, error)
	TouchAPIKey(ctx context.Context, apiKeyID string, at time.Time) error
	ConsumeQuota(ctx context.Context, userID string, tokens int) (bool, error)
	EnsureBootstrapKey(ctx context.Context, params BootstrapAPIKeyParams) error
}

type UsageStore interface {
	CreateUsageRecord(ctx context.Context, record UsageRecord) error
	GetUsageSummary(ctx context.Context, tenantID string) (*UsageStats, error)
	GetProviderUsageSummary(ctx context.Context, tenantID string) (map[string]ProviderUsageStats, error)
}

type TenantStore interface {
	EnsureTenant(ctx context.Context, params EnsureTenantParams) (*TenantRecord, error)
	ListTenants(ctx context.Context) ([]TenantRecord, error)
	GetTenant(ctx context.Context, idOrSlug string) (*TenantRecord, error)
	UpdateTenant(ctx context.Context, idOrSlug string, params UpdateTenantParams) (*TenantRecord, error)
	ListTenantProviders(ctx context.Context, tenantID string) ([]string, error)
	ReplaceTenantProviders(ctx context.Context, tenantID string, providerNames []string) error
}

type ResponseStore interface {
	CreateResponse(ctx context.Context, record ResponseRecord) error
	UpdateResponse(ctx context.Context, record ResponseRecord) error
	GetResponse(ctx context.Context, tenantID string, id string) (*ResponseRecord, error)
}

type TenantRecord struct {
	ID        string
	Slug      string
	Name      string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UserRecord struct {
	ID         string
	TenantID   string
	TenantSlug string
	APIKey     string
	Name       string
	Email      string
	Role       string
	Quota      int
	Used       int
	QPS        int
	Models     []string
	Status     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type UserStats struct {
	TotalUsers  int
	ActiveUsers int
	TotalQuota  int
	TotalUsed   int
}

type AuthIdentity struct {
	APIKeyID     string
	APIKey       string
	SecretHash   string
	APIStatus    string
	UserID       string
	UserName     string
	UserEmail    string
	UserStatus   string
	TenantID     string
	TenantSlug   string
	TenantStatus string
	Role         string
	Quota        int
	Used         int
	QPS          int
	Models       []string
}

type CreateUserParams struct {
	TenantID   string
	Name       string
	Email      string
	Role       string
	Quota      int
	QPS        int
	Models     []string
	Status     string
	APIKey     string
	SecretHash string
}

type UpdateUserParams struct {
	Role   *string
	Quota  *int
	QPS    *int
	Models *[]string
	Status *string
}

type BootstrapAPIKeyParams struct {
	TenantID   string
	Key        string
	SecretHash string
	Name       string
	Email      string
	Role       string
	Quota      int
	QPS        int
	Models     []string
}

type EnsureTenantParams struct {
	ID     string
	Slug   string
	Name   string
	Status string
}

type UpdateTenantParams struct {
	Name   *string
	Status *string
}

type UsageRecord struct {
	ID               string
	TenantID         string
	UserID           string
	APIKeyID         string
	ProviderName     string
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	Cost             float64
	LatencyMs        int64
	Status           string
	ErrorType        string
	CreatedAt        time.Time
}

type UsageStats struct {
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	TotalTokens     int64
	AvgLatencyMs    float64
}

type ProviderUsageStats struct {
	ProviderName    string
	TotalRequests   int64
	SuccessRequests int64
	FailedRequests  int64
	TotalTokens     int64
	AvgLatencyMs    float64
}

type ResponseRecord struct {
	ID           string
	TenantID     string
	UserID       string
	APIKeyID     string
	ProviderName string
	Model        string
	Status       string
	RequestBody  []byte
	ResponseBody []byte
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func IsAdminRole(role string) bool {
	return role == RoleTenantAdmin || role == RoleSuperAdmin
}

func HasRole(role string, allowed ...string) bool {
	for _, candidate := range allowed {
		if role == candidate {
			return true
		}
	}
	return false
}
