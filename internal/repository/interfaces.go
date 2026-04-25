package repository

import (
	"context"
	"errors"
	"time"
)

const (
	StatusActive   = "active"
	StatusInactive = "inactive"
	StatusRevoked  = "revoked"

	RoleSuperAdmin  = "super_admin"
	RoleTenantAdmin = "tenant_admin"
	RoleTenantUser  = "tenant_user"

	BudgetPolicyHardReject = "hard_reject"
	BudgetPolicySoftAlert  = "soft_alert"
	BudgetPolicyGrace      = "grace"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	UserStore
	APIKeyStore
	IdentityStore
	UsageStore
	TenantStore
	ResponseStore
	ProviderRegistryStore
	ProjectStore
	ServiceStore
	AuditLogStore
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

type BudgetCheckResult struct {
	Allowed   bool
	Scope     string
	Policy    string
	Remaining float64
}

type IdentityStore interface {
	Authenticate(ctx context.Context, key string) (*AuthIdentity, error)
	TouchAPIKey(ctx context.Context, apiKeyID string, at time.Time) error
	ConsumeQuota(ctx context.Context, userID string, tokens int) (bool, error)
	ConsumeAPIKeyBudget(ctx context.Context, apiKeyID string, cost float64) (bool, error)
	ConsumeProjectBudget(ctx context.Context, projectID string, cost float64) (bool, error)
	ConsumeTenantBudget(ctx context.Context, tenantID string, cost float64) (bool, error)
	CheckAPIKeyBudget(ctx context.Context, apiKeyID string, estimatedCost float64) (*BudgetCheckResult, error)
	CheckProjectBudget(ctx context.Context, projectID string, estimatedCost float64) (*BudgetCheckResult, error)
	CheckTenantBudget(ctx context.Context, tenantID string, estimatedCost float64) (*BudgetCheckResult, error)
	GetBudgetStatus(ctx context.Context, tenantID, projectID, apiKeyID string) ([]BudgetStatus, error)
	EnsureBootstrapKey(ctx context.Context, params BootstrapAPIKeyParams) error
}

type UsageStore interface {
	CreateUsageRecord(ctx context.Context, record UsageRecord) error
	GetUsageSummary(ctx context.Context, tenantID string) (*UsageStats, error)
	GetProviderUsageSummary(ctx context.Context, tenantID string) (map[string]ProviderUsageStats, error)
	GetProjectUsageSummary(ctx context.Context, tenantID, projectID string) (*UsageStats, error)
	GetUserUsageDetail(ctx context.Context, tenantID, userID string, startTime, endTime time.Time) ([]UsageRecord, error)
	GetUserUsageTrend(ctx context.Context, tenantID, userID string, days int) ([]DailyUsage, error)
	GetProjectUsageTrend(ctx context.Context, tenantID, projectID string, days int) ([]DailyUsage, error)
	GetTenantUsageTrend(ctx context.Context, tenantID string, days int) ([]DailyUsage, error)
	GetUsageSummaryFiltered(ctx context.Context, filter UsageFilter) (*UsageStats, error)
	GetUsageBreakdown(ctx context.Context, filter UsageFilter, dimension string) ([]UsageBreakdownRow, error)
	GetUsageTimeBuckets(ctx context.Context, filter UsageFilter, period string, limit int) ([]UsageTimeBucket, error)
}

type TenantStore interface {
	EnsureTenant(ctx context.Context, params EnsureTenantParams) (*TenantRecord, error)
	ListTenants(ctx context.Context) ([]TenantRecord, error)
	GetTenant(ctx context.Context, idOrSlug string) (*TenantRecord, error)
	UpdateTenant(ctx context.Context, idOrSlug string, params UpdateTenantParams) (*TenantRecord, error)
	ListTenantProviders(ctx context.Context, tenantID string) ([]string, error)
	ReplaceTenantProviders(ctx context.Context, tenantID string, providerNames []string) error
}

type APIKeyStore interface {
	CreateAPIKey(ctx context.Context, params CreateAPIKeyParams) (*APIKeyRecord, error)
	ListAPIKeys(ctx context.Context, tenantID string, filter APIKeyFilter) ([]APIKeyRecord, error)
	GetAPIKey(ctx context.Context, tenantID string, idOrKey string) (*APIKeyRecord, error)
	UpdateAPIKey(ctx context.Context, tenantID string, idOrKey string, params UpdateAPIKeyParams) (*APIKeyRecord, error)
	RotateAPIKey(ctx context.Context, tenantID string, idOrKey string, params RotateAPIKeyParams) (*APIKeyRecord, error)
}

type ServiceStore interface {
	CreateService(ctx context.Context, params CreateServiceParams) (*ServiceRecord, error)
	ListServices(ctx context.Context, tenantID string, filter ServiceFilter) ([]ServiceRecord, error)
	GetService(ctx context.Context, tenantID string, idOrPrefix string) (*ServiceRecord, error)
	GetServiceByPrefix(ctx context.Context, tenantID string, prefix string) (*ServiceRecord, error)
	UpdateService(ctx context.Context, tenantID string, idOrPrefix string, params UpdateServiceParams) (*ServiceRecord, error)
	CreateServiceVersion(ctx context.Context, tenantID string, params CreateServiceVersionParams) (*ServiceVersionRecord, error)
	ListServiceVersions(ctx context.Context, tenantID string, serviceID string) ([]ServiceVersionRecord, error)
	GetServiceVersion(ctx context.Context, tenantID string, serviceID string, versionOrID string) (*ServiceVersionRecord, error)
	PublishServiceVersion(ctx context.Context, tenantID string, serviceID string, params PublishServiceVersionParams) (*ServiceRecord, *ServiceVersionRecord, error)
	PromoteStagedServiceVersion(ctx context.Context, tenantID string, serviceID string) (*ServiceRecord, *ServiceVersionRecord, error)
	RollbackServiceVersion(ctx context.Context, tenantID string, serviceID string, params RollbackServiceVersionParams) (*ServiceRecord, *ServiceVersionRecord, error)
	CreateServiceSubscription(ctx context.Context, tenantID string, params CreateServiceSubscriptionParams) (*ServiceSubscriptionRecord, error)
	ListServiceSubscriptions(ctx context.Context, tenantID string, filter ServiceSubscriptionFilter) ([]ServiceSubscriptionRecord, error)
	GetServiceSubscription(ctx context.Context, tenantID string, id string) (*ServiceSubscriptionRecord, error)
	UpdateServiceSubscription(ctx context.Context, tenantID string, id string, params UpdateServiceSubscriptionParams) (*ServiceSubscriptionRecord, error)
}

type AuditLogStore interface {
	CreateAuditLog(ctx context.Context, record AuditLogRecord) error
	ListAuditLogs(ctx context.Context, tenantID string, filter AuditLogFilter) ([]AuditLogRecord, error)
}

type ResponseStore interface {
	CreateResponse(ctx context.Context, record ResponseRecord) error
	UpdateResponse(ctx context.Context, record ResponseRecord) error
	GetResponse(ctx context.Context, tenantID string, id string) (*ResponseRecord, error)
	ListResponses(ctx context.Context, tenantID string, filter ResponseFilter) ([]ResponseRecord, error)
}

type ResponseFilter struct {
	ProviderName string
	Model        string
	Status       string
	ProjectID    string
	APIKeyID     string
	UserID       string
	Query        string
	StartTime    time.Time
	EndTime      time.Time
	Limit        int
	Offset       int
}

type ProjectStore interface {
	CreateProject(ctx context.Context, params CreateProjectParams) (*ProjectRecord, error)
	ListProjects(ctx context.Context, tenantID string) ([]ProjectRecord, error)
	GetProject(ctx context.Context, tenantID string, idOrSlug string) (*ProjectRecord, error)
	UpdateProject(ctx context.Context, tenantID string, idOrSlug string, params UpdateProjectParams) (*ProjectRecord, error)
}

type ProviderRegistryStore interface {
	ListProviderRegistry(ctx context.Context) ([]ProviderRegistryRecord, error)
	GetProviderRegistry(ctx context.Context, name string) (*ProviderRegistryRecord, error)
	UpsertProviderRegistry(ctx context.Context, record ProviderRegistryRecord) error
	UpdateProviderRegistry(ctx context.Context, name string, params UpdateProviderRegistryParams) (*ProviderRegistryRecord, error)
	DeleteProviderRegistry(ctx context.Context, name string) error
}

type TenantRecord struct {
	ID           string
	Slug         string
	Name         string
	Status       string
	BudgetUSD    float64
	SpentUSD     float64
	BudgetPolicy string
	Policy       *ServicePolicyConfig
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UserRecord struct {
	ID           string
	TenantID     string
	TenantSlug   string
	APIKey       string
	ProjectID    string
	Name         string
	Email        string
	Role         string
	Quota        int
	Used         int
	QPS          int
	KeyBudgetUSD float64
	KeySpentUSD  float64
	Models       []string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UserStats struct {
	TotalUsers  int
	ActiveUsers int
	TotalQuota  int
	TotalUsed   int
}

type AuthIdentity struct {
	APIKeyID           string
	APIKey             string
	SecretHash         string
	APIStatus          string
	APIKeyModels       []string
	APIKeyProviders    []string
	APIKeyServices     []string
	APIKeyRateLimitQPS int
	ProjectID          string
	ProjectSlug        string
	ProjectName        string
	ProjectStatus      string
	ProjectBudgetUSD   float64
	ProjectSpentUSD    float64
	ProjectBudgetPolicy string
	APIKeyBudgetUSD    float64
	APIKeySpentUSD     float64
	APIKeyBudgetPolicy string
	UserID             string
	UserName           string
	UserEmail          string
	UserStatus         string
	TenantID           string
	TenantSlug         string
	TenantStatus       string
	TenantBudgetUSD    float64
	TenantSpentUSD     float64
	TenantBudgetPolicy string
	Role               string
	Quota              int
	Used               int
	QPS                int
	Models             []string
}

type BudgetStatus struct {
	Scope       string  `json:"scope"`
	ID          string  `json:"id"`
	BudgetUSD   float64 `json:"budget_usd"`
	SpentUSD    float64 `json:"spent_usd"`
	OverageUSD  float64 `json:"overage_usd"`
	Policy      string  `json:"policy"`
	Utilization float64 `json:"utilization"`
	IsExhausted bool    `json:"is_exhausted"`
}

type CreateUserParams struct {
	TenantID     string
	ProjectID    string
	Name         string
	Email        string
	Role         string
	Quota        int
	QPS          int
	KeyBudgetUSD float64
	Models       []string
	Status       string
	APIKey       string
	SecretHash   string
}

type UpdateUserParams struct {
	Role         *string
	Quota        *int
	QPS          *int
	ProjectID    *string
	KeyBudgetUSD *float64
	Models       *[]string
	Status       *string
}

type BootstrapAPIKeyParams struct {
	TenantID     string
	ProjectID    string
	Key          string
	SecretHash   string
	Name         string
	Email        string
	Role         string
	Quota        int
	QPS          int
	KeyBudgetUSD float64
	Models       []string
}

type EnsureTenantParams struct {
	ID        string
	Slug      string
	Name      string
	Status    string
	BudgetUSD float64
	Policy    *ServicePolicyConfig
}

type UpdateTenantParams struct {
	Name         *string
	Status       *string
	BudgetUSD    *float64
	BudgetPolicy *string
	Policy       *ServicePolicyConfig
}

type APIKeyRecord struct {
	ID               string
	TenantID         string
	TenantSlug       string
	UserID           string
	UserName         string
	UserEmail        string
	ProjectID        string
	ProjectSlug      string
	Key              string
	Status           string
	BudgetUSD        float64
	SpentUSD         float64
	BudgetPolicy     string
	RateLimitQPS     int
	AllowedModels    []string
	AllowedProviders []string
	AllowedServices  []string
	LastUsedAt       *time.Time
	RevokedAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type APIKeyFilter struct {
	UserID    string
	ProjectID string
	Status    string
}

type CreateAPIKeyParams struct {
	UserID           string
	ProjectID        string
	Key              string
	SecretHash       string
	Status           string
	BudgetUSD        float64
	RateLimitQPS     int
	AllowedModels    []string
	AllowedProviders []string
	AllowedServices  []string
}

type UpdateAPIKeyParams struct {
	ProjectID        *string
	Status           *string
	BudgetUSD        *float64
	BudgetPolicy     *string
	RateLimitQPS     *int
	AllowedModels    *[]string
	AllowedProviders *[]string
	AllowedServices  *[]string
	RevokedAt        *time.Time
}

type RotateAPIKeyParams struct {
	NewKey        string
	NewSecretHash string
}

type ProjectRecord struct {
	ID           string
	TenantID     string
	TenantSlug   string
	Slug         string
	Name         string
	Status       string
	BudgetUSD    float64
	SpentUSD     float64
	BudgetPolicy string
	Policy       *ServicePolicyConfig
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateProjectParams struct {
	TenantID  string
	Slug      string
	Name      string
	Status    string
	BudgetUSD float64
	Policy    *ServicePolicyConfig
}

type UpdateProjectParams struct {
	Name         *string
	Status       *string
	BudgetUSD    *float64
	BudgetPolicy *string
	Policy       *ServicePolicyConfig
}

type UsageRecord struct {
	ID               string
	TenantID         string
	ProjectID        string
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
	TotalRequests   int64   `json:"total_requests"`
	SuccessRequests int64   `json:"success_requests"`
	FailedRequests  int64   `json:"failed_requests"`
	TotalTokens     int64   `json:"total_tokens"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
}

type ProviderUsageStats struct {
	ProviderName    string  `json:"provider_name"`
	TotalRequests   int64   `json:"total_requests"`
	SuccessRequests int64   `json:"success_requests"`
	FailedRequests  int64   `json:"failed_requests"`
	TotalTokens     int64   `json:"total_tokens"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
}

type DailyUsage struct {
	Date            string  `json:"date"`
	TotalRequests   int64   `json:"total_requests"`
	SuccessRequests int64   `json:"success_requests"`
	FailedRequests  int64   `json:"failed_requests"`
	TotalTokens     int64   `json:"total_tokens"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
}

type UsageFilter struct {
	TenantID     string
	ProjectID    string
	UserID       string
	APIKeyID     string
	ProviderName string
	Model        string
	StartTime    time.Time
	EndTime      time.Time
}

type UsageBreakdownRow struct {
	Dimension       string  `json:"dimension"`
	TotalRequests   int64   `json:"total_requests"`
	SuccessRequests int64   `json:"success_requests"`
	FailedRequests  int64   `json:"failed_requests"`
	TotalTokens     int64   `json:"total_tokens"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
}

type UsageTimeBucket struct {
	Bucket          string  `json:"bucket"`
	TotalRequests   int64   `json:"total_requests"`
	SuccessRequests int64   `json:"success_requests"`
	FailedRequests  int64   `json:"failed_requests"`
	TotalTokens     int64   `json:"total_tokens"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
}

type ResponseRecord struct {
	ID             string
	TenantID       string
	ProjectID      string
	UserID         string
	APIKeyID       string
	ProviderName   string
	Model          string
	Status         string
	RequestBody    []byte
	ResponseBody   []byte
	RouteTraceBody []byte
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type ServiceRecord struct {
	ID                 string
	TenantID           string
	ProjectID          string
	ProjectSlug        string
	Name               string
	RequestPrefix      string
	Description        string
	DefaultProvider    string
	DefaultModel       string
	PublishStatus      string
	PublishedVersionID string
	StagedVersionID    string
	Enabled            bool
	Config             ServiceConfig
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ServiceConfig struct {
	Surfaces       []string              `json:"surfaces,omitempty"`
	PromptTemplate *PromptTemplateConfig `json:"prompt_template,omitempty"`
	Policy         *ServicePolicyConfig  `json:"policy,omitempty"`
	Metadata       map[string]any        `json:"metadata,omitempty"`
}

type PromptTemplateConfig struct {
	SystemTemplate string                   `json:"system_template,omitempty"`
	UserTemplate   string                   `json:"user_template,omitempty"`
	Variables      []PromptTemplateVariable `json:"variables,omitempty"`
}

type PromptTemplateVariable struct {
	Name        string `json:"name"`
	Default     string `json:"default,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

type ServicePolicyConfig struct {
	Enabled  bool              `json:"enabled,omitempty"`
	Request  *GuardrailRuleSet `json:"request,omitempty"`
	Response *GuardrailRuleSet `json:"response,omitempty"`
}

type GuardrailRuleSet struct {
	AllowModels    []string `json:"allow_models,omitempty"`
	BlockModels    []string `json:"block_models,omitempty"`
	BlockTerms     []string `json:"block_terms,omitempty"`
	BlockRegex     []string `json:"block_regex,omitempty"`
	RedactTerms    []string `json:"redact_terms,omitempty"`
	MaxInputChars  int      `json:"max_input_chars,omitempty"`
	MaxOutputChars int      `json:"max_output_chars,omitempty"`
}

type CreateServiceParams struct {
	TenantID        string
	ProjectID       string
	Name            string
	RequestPrefix   string
	Description     string
	DefaultProvider string
	DefaultModel    string
	Enabled         bool
	Config          ServiceConfig
}

type UpdateServiceParams struct {
	ProjectID       *string
	Name            *string
	RequestPrefix   *string
	Description     *string
	DefaultProvider *string
	DefaultModel    *string
	Enabled         *bool
	Config          *ServiceConfig
}

type ServiceFilter struct {
	ProjectID     string
	PublishStatus string
	Enabled       *bool
}

type ServiceSnapshot struct {
	Name            string        `json:"name"`
	RequestPrefix   string        `json:"request_prefix"`
	Description     string        `json:"description,omitempty"`
	DefaultProvider string        `json:"default_provider,omitempty"`
	DefaultModel    string        `json:"default_model,omitempty"`
	Enabled         bool          `json:"enabled"`
	Config          ServiceConfig `json:"config"`
}

type ServiceVersionRecord struct {
	ID        string
	ServiceID string
	TenantID  string
	Version   int
	Status    string
	Snapshot  ServiceSnapshot
	CreatedAt time.Time
	UpdatedAt time.Time
}

type CreateServiceVersionParams struct {
	ServiceID string
	Snapshot  ServiceSnapshot
}

type PublishServiceVersionParams struct {
	VersionID string
	Mode      string
}

type RollbackServiceVersionParams struct {
	VersionID string
}

type ServiceSubscriptionRecord struct {
	ID                    string
	TenantID              string
	ServiceID             string
	ProjectID             string
	ProjectSlug           string
	ConsumerName          string
	ConsumerEmail         string
	ConsumerUserID        string
	Status                string
	RequestedBudgetUSD    float64
	RequestedRateLimitQPS int
	AllowedSurfaces       []string
	ApprovedAPIKeyID      string
	ApprovedUserID        string
	ReviewNote            string
	ApprovedAt            *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type AuditLogRecord struct {
	ID            string
	TenantID      string
	ActorUserID   string
	ActorAPIKeyID string
	ActorRole     string
	Action        string
	ResourceType  string
	ResourceID    string
	RequestID     string
	IPAddress     string
	Payload       []byte
	CreatedAt     time.Time
}

type AuditLogFilter struct {
	Action       string
	ResourceType string
	ResourceID   string
	ActorUserID  string
	StartTime    time.Time
	EndTime      time.Time
	Limit        int
}

type CreateServiceSubscriptionParams struct {
	ServiceID             string
	ProjectID             string
	ConsumerName          string
	ConsumerEmail         string
	ConsumerUserID        string
	RequestedBudgetUSD    float64
	RequestedRateLimitQPS int
	AllowedSurfaces       []string
}

type ServiceSubscriptionFilter struct {
	ServiceID string
	ProjectID string
	Status    string
}

type UpdateServiceSubscriptionParams struct {
	Status           *string
	ReviewNote       *string
	ApprovedAPIKeyID *string
	ApprovedUserID   *string
}

type ProviderRegistryRecord struct {
	Name                     string
	Type                     string
	Vendor                   string
	BaseURL                  string
	Endpoint                 string
	Model                    string
	Enabled                  bool
	Drain                    bool
	HealthStatus             string
	RoutingWeight            int
	SupportsChat             bool
	SupportsResponses        bool
	SupportsMessages         bool
	SupportsStream           bool
	SupportsTools            bool
	SupportsImages           bool
	SupportsStructuredOutput bool
	SupportsLongContext      bool
	SupportsEmbeddings       bool
	RuntimeConfig            *ProviderRuntimeConfig
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type ProviderRuntimeConfig struct {
	GRPCTarget    string            `json:"grpc_target,omitempty"`
	GRPCUseTLS    bool              `json:"grpc_use_tls,omitempty"`
	GRPCAuthority string            `json:"grpc_authority,omitempty"`
	APIKey        string            `json:"api_key,omitempty"`
	PriceInput    float64           `json:"price_input,omitempty"`
	PriceOutput   float64           `json:"price_output,omitempty"`
	MaxTokens     int               `json:"max_tokens,omitempty"`
	Timeout       int               `json:"timeout,omitempty"`
	Enabled       bool              `json:"enabled,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	ExtraBody     map[string]any    `json:"extra_body,omitempty"`
}

type UpdateProviderRegistryParams struct {
	Enabled                  *bool
	Drain                    *bool
	HealthStatus             *string
	RoutingWeight            *int
	SupportsChat             *bool
	SupportsResponses        *bool
	SupportsMessages         *bool
	SupportsStream           *bool
	SupportsTools            *bool
	SupportsImages           *bool
	SupportsStructuredOutput *bool
	SupportsLongContext      *bool
	SupportsEmbeddings       *bool
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
