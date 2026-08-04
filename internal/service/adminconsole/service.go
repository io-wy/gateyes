package adminconsole

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/provider"
)

var (
	ErrForbidden       = errors.New("forbidden")
	ErrMissingUserID   = errors.New("user_id is required")
	ErrExceededParent  = errors.New("virtual key exceeds parent api key limits")
	ErrInvalidTenantID = errors.New("tenant_id is required")
)

type Service struct {
	store              repository.Store
	catalogSvc         *catalog.Service
	providerRuntimeSvc *provider.RuntimeRegistryService
}

func New(store repository.Store, catalogSvc *catalog.Service, providerRuntimeSvc *provider.RuntimeRegistryService) *Service {
	return &Service{store: store, catalogSvc: catalogSvc, providerRuntimeSvc: providerRuntimeSvc}
}

type IdentityView struct {
	TenantID        string   `json:"tenant_id"`
	TenantSlug      string   `json:"tenant_slug"`
	UserID          string   `json:"user_id"`
	UserName        string   `json:"user_name"`
	UserEmail       string   `json:"user_email"`
	Role            string   `json:"role"`
	Permissions     []string `json:"permissions"`
	APIKeyID        string   `json:"api_key_id"`
	APIKey          string   `json:"api_key"`
	ProjectID       string   `json:"project_id"`
	ProjectSlug     string   `json:"project_slug"`
	VirtualKeyID    string   `json:"virtual_key_id"`
	AllowedModels   []string `json:"allowed_models"`
	AllowedServices []string `json:"allowed_services"`
}

type CreateAPIKeyInput struct {
	UserID           string
	ProjectID        string
	BudgetUSD        float64
	RateLimitQPS     int
	AllowedModels    []string
	AllowedProviders []string
	AllowedServices  []string
	ExpiresAt        *time.Time
}

type APIKeyWithSecret struct {
	Record *repository.APIKeyRecord
	Secret string
	Token  string
	OldKey string
}

type CreateVirtualKeyInput struct {
	UserID           string
	APIKeyID         string
	ProjectID        string
	Name             string
	BudgetUSD        float64
	BudgetPolicy     string
	RateLimitQPS     int
	AllowedModels    []string
	AllowedProviders []string
	CallbackURL      string
	ExpiresAt        *time.Time
}

type VirtualKeyWithSecret struct {
	Record *repository.VirtualKeyRecord
	Secret string
	Token  string
}

type DashboardSummary struct {
	UsageStats      *repository.UsageStats
	ActiveProviders int
}

type CatalogView struct {
	TenantID  string
	Providers []provider.ProviderView
	Services  []repository.ServiceRecord
}

func (s *Service) Me(ctx context.Context, identity *repository.AuthIdentity) (*IdentityView, error) {
	permissionSet := make(map[string]struct{})
	for _, permission := range fallbackPermissions(identity.Role) {
		permissionSet[permission] = struct{}{}
	}
	if identity.UserID != "" {
		if records, err := s.store.GetUserPermissions(ctx, identity.UserID); err == nil {
			for _, record := range records {
				permissionSet[record.Code] = struct{}{}
			}
		}
	}
	permissions := make([]string, 0, len(permissionSet))
	for permission := range permissionSet {
		permissions = append(permissions, permission)
	}
	sort.Strings(permissions)
	return &IdentityView{
		TenantID:        identity.TenantID,
		TenantSlug:      identity.TenantSlug,
		UserID:          identity.UserID,
		UserName:        identity.UserName,
		UserEmail:       identity.UserEmail,
		Role:            identity.Role,
		Permissions:     permissions,
		APIKeyID:        identity.APIKeyID,
		APIKey:          identity.APIKey,
		ProjectID:       identity.ProjectID,
		ProjectSlug:     identity.ProjectSlug,
		VirtualKeyID:    identity.VirtualKeyID,
		AllowedModels:   identity.APIKeyModels,
		AllowedServices: identity.APIKeyServices,
	}, nil
}

func (s *Service) CreateAPIKey(ctx context.Context, identity *repository.AuthIdentity, input CreateAPIKeyInput) (*APIKeyWithSecret, error) {
	tenantID, err := scopedTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	userID := input.UserID
	if isTenantUser(identity) {
		userID = identity.UserID
	}
	if userID == "" {
		return nil, ErrMissingUserID
	}
	user, err := s.store.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	apiKey, err := repository.GenerateToken("gk-", 8)
	if err != nil {
		return nil, err
	}
	apiSecret, err := repository.GenerateToken("gs-", 16)
	if err != nil {
		return nil, err
	}
	record, err := s.store.CreateAPIKey(ctx, repository.CreateAPIKeyParams{
		UserID:           user.ID,
		ProjectID:        input.ProjectID,
		Key:              apiKey,
		SecretHash:       repository.HashSecret(apiSecret),
		Status:           repository.StatusActive,
		BudgetUSD:        input.BudgetUSD,
		RateLimitQPS:     input.RateLimitQPS,
		AllowedModels:    input.AllowedModels,
		AllowedProviders: input.AllowedProviders,
		AllowedServices:  input.AllowedServices,
		ExpiresAt:        input.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}
	return &APIKeyWithSecret{Record: record, Secret: apiSecret, Token: record.Key + ":" + apiSecret}, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, identity *repository.AuthIdentity, filter repository.APIKeyFilter) ([]repository.APIKeyRecord, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	if isTenantUser(identity) {
		filter.UserID = identity.UserID
	}
	return s.store.ListAPIKeys(ctx, tenantID, filter)
}

func (s *Service) GetAPIKey(ctx context.Context, identity *repository.AuthIdentity, id string) (*repository.APIKeyRecord, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	record, err := s.store.GetAPIKey(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if !ownsUserResource(identity, record.UserID) {
		return nil, repository.ErrNotFound
	}
	return record, nil
}

func (s *Service) UpdateAPIKey(ctx context.Context, identity *repository.AuthIdentity, id string, params repository.UpdateAPIKeyParams) (*repository.APIKeyRecord, error) {
	if isTenantUser(identity) && (params.ProjectID != nil || params.BudgetUSD != nil || params.BudgetPolicy != nil || params.RateLimitQPS != nil ||
		params.AllowedModels != nil || params.AllowedProviders != nil || params.AllowedServices != nil || params.ExpiresAt != nil) {
		return nil, ErrForbidden
	}
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	record, err := s.GetAPIKey(ctx, identity, id)
	if err != nil {
		return nil, err
	}
	return s.store.UpdateAPIKey(ctx, tenantID, record.ID, params)
}

func (s *Service) RotateAPIKey(ctx context.Context, identity *repository.AuthIdentity, id string) (*APIKeyWithSecret, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	record, err := s.GetAPIKey(ctx, identity, id)
	if err != nil {
		return nil, err
	}
	newKey, err := repository.GenerateToken("gk-", 8)
	if err != nil {
		return nil, err
	}
	newSecret, err := repository.GenerateToken("gs-", 16)
	if err != nil {
		return nil, err
	}
	rotated, err := s.store.RotateAPIKey(ctx, tenantID, record.ID, repository.RotateAPIKeyParams{
		NewKey:        newKey,
		NewSecretHash: repository.HashSecret(newSecret),
	})
	if err != nil {
		return nil, err
	}
	return &APIKeyWithSecret{Record: rotated, Secret: newSecret, Token: rotated.Key + ":" + newSecret, OldKey: record.Key}, nil
}

func (s *Service) RevokeAPIKey(ctx context.Context, identity *repository.AuthIdentity, id string) (*repository.APIKeyRecord, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	record, err := s.GetAPIKey(ctx, identity, id)
	if err != nil {
		return nil, err
	}
	status := repository.StatusRevoked
	now := time.Now().UTC()
	return s.store.UpdateAPIKey(ctx, tenantID, record.ID, repository.UpdateAPIKeyParams{
		Status:    &status,
		RevokedAt: &now,
	})
}

func (s *Service) ListVirtualKeys(ctx context.Context, identity *repository.AuthIdentity, filter repository.VirtualKeyFilter) ([]repository.VirtualKeyRecord, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	if isTenantUser(identity) {
		filter.UserID = identity.UserID
	}
	return s.store.ListVirtualKeys(ctx, tenantID, filter)
}

func (s *Service) GetVirtualKey(ctx context.Context, identity *repository.AuthIdentity, id string) (*repository.VirtualKeyRecord, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	record, err := s.store.GetVirtualKey(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if !ownsUserResource(identity, record.UserID) {
		return nil, repository.ErrNotFound
	}
	return record, nil
}

func (s *Service) CreateVirtualKey(ctx context.Context, identity *repository.AuthIdentity, input CreateVirtualKeyInput) (*VirtualKeyWithSecret, error) {
	tenantID, err := scopedTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	userID := input.UserID
	if isTenantUser(identity) {
		userID = identity.UserID
	}
	if userID == "" {
		return nil, ErrMissingUserID
	}
	parentKey, err := s.store.GetAPIKey(ctx, tenantID, input.APIKeyID)
	if err != nil {
		return nil, err
	}
	if isTenantUser(identity) {
		if parentKey.UserID != userID {
			return nil, repository.ErrNotFound
		}
		if !virtualKeyWithinAPIKeyLimits(input.BudgetUSD, input.RateLimitQPS, input.AllowedModels, input.AllowedProviders, *parentKey) {
			return nil, ErrExceededParent
		}
	}
	vk, err := repository.GenerateToken("vk-", 8)
	if err != nil {
		return nil, err
	}
	vkSecret, err := repository.GenerateToken("vs-", 16)
	if err != nil {
		return nil, err
	}
	record, err := s.store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID:         tenantID,
		UserID:           userID,
		APIKeyID:         input.APIKeyID,
		ProjectID:        input.ProjectID,
		Name:             input.Name,
		Key:              vk,
		SecretHash:       repository.HashSecret(vkSecret),
		BudgetUSD:        input.BudgetUSD,
		BudgetPolicy:     input.BudgetPolicy,
		RateLimitQPS:     input.RateLimitQPS,
		AllowedModels:    input.AllowedModels,
		AllowedProviders: input.AllowedProviders,
		CallbackURL:      input.CallbackURL,
		ExpiresAt:        input.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}
	return &VirtualKeyWithSecret{Record: record, Secret: vkSecret, Token: record.Key + ":" + vkSecret}, nil
}

func (s *Service) UpdateVirtualKey(ctx context.Context, identity *repository.AuthIdentity, id string, params repository.UpdateVirtualKeyParams) (*repository.VirtualKeyRecord, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	current, err := s.GetVirtualKey(ctx, identity, id)
	if err != nil {
		return nil, err
	}
	if isTenantUser(identity) {
		parentKey, err := s.store.GetAPIKey(ctx, tenantID, current.APIKeyID)
		if err != nil {
			return nil, err
		}
		if !virtualKeyUpdateWithinAPIKeyLimits(params, *parentKey) {
			return nil, ErrExceededParent
		}
	}
	return s.store.UpdateVirtualKey(ctx, tenantID, current.ID, params)
}

func (s *Service) DeleteVirtualKey(ctx context.Context, identity *repository.AuthIdentity, id string) error {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return err
	}
	current, err := s.GetVirtualKey(ctx, identity, id)
	if err != nil {
		return err
	}
	return s.store.DeleteVirtualKey(ctx, tenantID, current.ID)
}

func (s *Service) ListResponses(ctx context.Context, identity *repository.AuthIdentity, filter repository.ResponseFilter) ([]repository.ResponseRecord, int, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, 0, err
	}
	if isTenantUser(identity) {
		filter.UserID = identity.UserID
	}
	total, err := s.store.CountResponses(ctx, tenantID, filter)
	if err != nil {
		return nil, 0, err
	}
	items, err := s.store.ListResponses(ctx, tenantID, filter)
	return items, total, err
}

func (s *Service) GetResponse(ctx context.Context, identity *repository.AuthIdentity, id string) (*repository.ResponseRecord, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	record, err := s.store.GetResponse(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if !ownsUserResource(identity, record.UserID) {
		return nil, repository.ErrNotFound
	}
	return record, nil
}

func (s *Service) Dashboard(ctx context.Context, identity *repository.AuthIdentity) (*DashboardSummary, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	var stats *repository.UsageStats
	if isTenantUser(identity) {
		stats, err = s.store.GetUsageSummaryFiltered(ctx, repository.UsageFilter{
			TenantID:  tenantID,
			UserID:    identity.UserID,
			StartTime: time.Now().UTC().AddDate(0, 0, -30),
		})
	} else {
		stats, err = s.store.GetUsageSummary(ctx, tenantID)
	}
	if err != nil {
		return nil, err
	}
	activeProviders := 0
	if !isTenantUser(identity) {
		providers, err := s.providerRuntimeSvc.List(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for _, p := range providers {
			if providerStatus(p.Stats) == provider.ProviderHealthHealthy {
				activeProviders++
			}
		}
	}
	return &DashboardSummary{UsageStats: stats, ActiveProviders: activeProviders}, nil
}

func (s *Service) UsageSummary(ctx context.Context, identity *repository.AuthIdentity, filter repository.UsageFilter) (*repository.UsageStats, repository.UsageFilter, error) {
	resolved, err := s.usageFilter(identity, filter)
	if err != nil {
		return nil, repository.UsageFilter{}, err
	}
	stats, err := s.store.GetUsageSummaryFiltered(ctx, resolved)
	return stats, resolved, err
}

func (s *Service) UsageBreakdown(ctx context.Context, identity *repository.AuthIdentity, filter repository.UsageFilter, dimension string) ([]repository.UsageBreakdownRow, repository.UsageFilter, error) {
	resolved, err := s.usageFilter(identity, filter)
	if err != nil {
		return nil, repository.UsageFilter{}, err
	}
	rows, err := s.store.GetUsageBreakdown(ctx, resolved, dimension)
	return rows, resolved, err
}

func (s *Service) UsageTrend(ctx context.Context, identity *repository.AuthIdentity, filter repository.UsageFilter, period string, limit int) ([]repository.UsageTimeBucket, repository.UsageFilter, error) {
	resolved, err := s.usageFilter(identity, filter)
	if err != nil {
		return nil, repository.UsageFilter{}, err
	}
	rows, err := s.store.GetUsageTimeBuckets(ctx, resolved, period, limit)
	return rows, resolved, err
}

func (s *Service) ListServices(ctx context.Context, identity *repository.AuthIdentity, filter repository.ServiceFilter) ([]repository.ServiceRecord, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	if isTenantUser(identity) {
		enabled := true
		filter.Enabled = &enabled
		filter.PublishStatus = "published"
	}
	return s.catalogSvc.ListServices(ctx, tenantID, filter)
}

func (s *Service) GetService(ctx context.Context, identity *repository.AuthIdentity, id string) (*catalog.ServiceWithVersions, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	result, err := s.catalogSvc.GetServiceWithVersions(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if isTenantUser(identity) && (!result.Service.Enabled || result.Service.PublishStatus != "published") {
		return nil, repository.ErrNotFound
	}
	return result, nil
}

func (s *Service) Catalog(ctx context.Context, identity *repository.AuthIdentity, projectID, publishStatus string) (*CatalogView, error) {
	tenantID, err := adminTenantID(identity, "")
	if err != nil {
		return nil, err
	}
	var providers []provider.ProviderView
	if !isTenantUser(identity) {
		providers, err = s.providerRuntimeSvc.List(ctx, tenantID)
		if err != nil {
			return nil, err
		}
	}
	if isTenantUser(identity) {
		publishStatus = "published"
	}
	services, err := s.catalogSvc.ListServices(ctx, tenantID, repository.ServiceFilter{
		ProjectID:     projectID,
		PublishStatus: publishStatus,
	})
	if err != nil {
		return nil, err
	}
	return &CatalogView{TenantID: tenantID, Providers: providers, Services: services}, nil
}

func (s *Service) usageFilter(identity *repository.AuthIdentity, filter repository.UsageFilter) (repository.UsageFilter, error) {
	tenantID, err := scopedTenantID(identity, filter.TenantID)
	if err != nil {
		return repository.UsageFilter{}, err
	}
	filter.TenantID = tenantID
	if isTenantUser(identity) {
		filter.UserID = identity.UserID
	}
	if filter.StartTime.IsZero() && filter.EndTime.IsZero() {
		filter.StartTime = time.Now().UTC().AddDate(0, 0, -30)
	}
	return filter, nil
}

func scopedTenantID(identity *repository.AuthIdentity, requested string) (string, error) {
	if identity.Role == repository.RoleSuperAdmin {
		if requested != "" {
			return requested, nil
		}
		if identity.TenantID == "" {
			return "", ErrInvalidTenantID
		}
	}
	return identity.TenantID, nil
}

func adminTenantID(identity *repository.AuthIdentity, requested string) (string, error) {
	if identity.Role == repository.RoleSuperAdmin {
		if requested != "" {
			return requested, nil
		}
		return identity.TenantID, nil
	}
	return identity.TenantID, nil
}

func isTenantUser(identity *repository.AuthIdentity) bool {
	return identity != nil && identity.Role == repository.RoleTenantUser
}

func ownsUserResource(identity *repository.AuthIdentity, ownerUserID string) bool {
	return !isTenantUser(identity) || ownerUserID == identity.UserID
}

func virtualKeyWithinAPIKeyLimits(budgetUSD float64, rateLimitQPS int, models, providers []string, parent repository.APIKeyRecord) bool {
	if parent.BudgetUSD > 0 && budgetUSD > parent.BudgetUSD {
		return false
	}
	if parent.RateLimitQPS > 0 && rateLimitQPS > parent.RateLimitQPS {
		return false
	}
	if !stringSubset(models, parent.AllowedModels) {
		return false
	}
	return stringSubset(providers, parent.AllowedProviders)
}

func virtualKeyUpdateWithinAPIKeyLimits(params repository.UpdateVirtualKeyParams, parent repository.APIKeyRecord) bool {
	if params.BudgetUSD != nil && parent.BudgetUSD > 0 && *params.BudgetUSD > parent.BudgetUSD {
		return false
	}
	if params.RateLimitQPS != nil && parent.RateLimitQPS > 0 && *params.RateLimitQPS > parent.RateLimitQPS {
		return false
	}
	if params.AllowedModels != nil && !stringSubset(*params.AllowedModels, parent.AllowedModels) {
		return false
	}
	return params.AllowedProviders == nil || stringSubset(*params.AllowedProviders, parent.AllowedProviders)
}

func stringSubset(candidate []string, allowed []string) bool {
	if len(candidate) == 0 || len(allowed) == 0 {
		return true
	}
	seen := make(map[string]struct{}, len(allowed))
	for _, item := range allowed {
		seen[item] = struct{}{}
	}
	for _, item := range candidate {
		if _, ok := seen[item]; !ok {
			return false
		}
	}
	return true
}

func fallbackPermissions(role string) []string {
	switch role {
	case repository.RoleSuperAdmin:
		return []string{"provider:read", "provider:write", "api_key:read", "api_key:write", "user:read", "user:write", "tenant:read", "tenant:write", "project:read", "project:write", "service:read", "service:write", "virtual_key:read", "virtual_key:write", "usage:read", "response:read", "budget:read", "audit:read", "config:write"}
	case repository.RoleTenantAdmin:
		return []string{"provider:read", "provider:write", "api_key:read", "api_key:write", "user:read", "user:write", "tenant:read", "tenant:write", "project:read", "project:write", "service:read", "service:write", "virtual_key:read", "virtual_key:write", "usage:read", "response:read", "budget:read", "audit:read"}
	case repository.RoleTenantUser:
		return []string{"api_key:read", "api_key:write", "service:read", "virtual_key:read", "virtual_key:write", "usage:read", "response:read"}
	default:
		return nil
	}
}

func providerStatus(stats *provider.ProviderStats) string {
	if stats == nil {
		return "unknown"
	}
	return stats.Status
}
