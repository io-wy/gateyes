package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
)

type mockStore struct {
	service       *repository.ServiceRecord
	version       *repository.ServiceVersionRecord
	subscriptions []repository.ServiceSubscriptionRecord
}

func (m *mockStore) CreateService(ctx context.Context, params repository.CreateServiceParams) (*repository.ServiceRecord, error) {
	return m.service, nil
}
func (m *mockStore) ListServices(ctx context.Context, tenantID string, filter repository.ServiceFilter) ([]repository.ServiceRecord, error) {
	return nil, nil
}
func (m *mockStore) GetService(ctx context.Context, tenantID string, idOrPrefix string) (*repository.ServiceRecord, error) {
	return m.service, nil
}
func (m *mockStore) GetServiceByPrefix(ctx context.Context, tenantID string, prefix string) (*repository.ServiceRecord, error) {
	return m.service, nil
}
func (m *mockStore) UpdateService(ctx context.Context, tenantID string, idOrPrefix string, params repository.UpdateServiceParams) (*repository.ServiceRecord, error) {
	return m.service, nil
}
func (m *mockStore) DeleteService(ctx context.Context, tenantID string, idOrPrefix string) error {
	return nil
}
func (m *mockStore) CreateServiceVersion(ctx context.Context, tenantID string, params repository.CreateServiceVersionParams) (*repository.ServiceVersionRecord, error) {
	return m.version, nil
}
func (m *mockStore) ListServiceVersions(ctx context.Context, tenantID string, serviceID string) ([]repository.ServiceVersionRecord, error) {
	return nil, nil
}
func (m *mockStore) GetServiceVersion(ctx context.Context, tenantID string, serviceID string, versionOrID string) (*repository.ServiceVersionRecord, error) {
	return m.version, nil
}
func (m *mockStore) PublishServiceVersion(ctx context.Context, tenantID string, serviceID string, params repository.PublishServiceVersionParams) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error) {
	return m.service, m.version, nil
}
func (m *mockStore) PromoteStagedServiceVersion(ctx context.Context, tenantID string, serviceID string) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error) {
	return m.service, m.version, nil
}
func (m *mockStore) RollbackServiceVersion(ctx context.Context, tenantID string, serviceID string, params repository.RollbackServiceVersionParams) (*repository.ServiceRecord, *repository.ServiceVersionRecord, error) {
	return m.service, m.version, nil
}
func (m *mockStore) CreateServiceSubscription(ctx context.Context, tenantID string, params repository.CreateServiceSubscriptionParams) (*repository.ServiceSubscriptionRecord, error) {
	return nil, nil
}
func (m *mockStore) ListServiceSubscriptions(ctx context.Context, tenantID string, filter repository.ServiceSubscriptionFilter) ([]repository.ServiceSubscriptionRecord, error) {
	return m.subscriptions, nil
}
func (m *mockStore) GetServiceSubscription(ctx context.Context, tenantID string, id string) (*repository.ServiceSubscriptionRecord, error) {
	return nil, nil
}
func (m *mockStore) UpdateServiceSubscription(ctx context.Context, tenantID string, id string, params repository.UpdateServiceSubscriptionParams) (*repository.ServiceSubscriptionRecord, error) {
	return nil, nil
}
func (m *mockStore) CreateResponse(ctx context.Context, record repository.ResponseRecord) error {
	return nil
}
func (m *mockStore) UpdateResponse(ctx context.Context, record repository.ResponseRecord) error {
	return nil
}
func (m *mockStore) GetResponse(ctx context.Context, tenantID string, id string) (*repository.ResponseRecord, error) {
	return nil, nil
}
func (m *mockStore) ListResponses(ctx context.Context, tenantID string, filter repository.ResponseFilter) ([]repository.ResponseRecord, error) {
	return nil, nil
}
func (m *mockStore) CountResponses(ctx context.Context, tenantID string, filter repository.ResponseFilter) (int, error) {
	return 0, nil
}
func (m *mockStore) CreateUser(ctx context.Context, params repository.CreateUserParams) (*repository.UserRecord, error) {
	return &repository.UserRecord{ID: "user-1"}, nil
}
func (m *mockStore) ListUsers(ctx context.Context, tenantID string) ([]repository.UserRecord, error) {
	return nil, nil
}
func (m *mockStore) GetUser(ctx context.Context, tenantID string, idOrAPIKey string) (*repository.UserRecord, error) {
	return nil, nil
}
func (m *mockStore) UpdateUser(ctx context.Context, tenantID string, idOrAPIKey string, params repository.UpdateUserParams) (*repository.UserRecord, error) {
	return nil, nil
}
func (m *mockStore) DeleteUser(ctx context.Context, tenantID string, idOrAPIKey string) error {
	return nil
}
func (m *mockStore) ResetUserUsage(ctx context.Context, tenantID string, idOrAPIKey string) (*repository.UserRecord, error) {
	return nil, nil
}
func (m *mockStore) Stats(ctx context.Context, tenantID string) (*repository.UserStats, error) {
	return nil, nil
}
func (m *mockStore) CreateAPIKey(ctx context.Context, params repository.CreateAPIKeyParams) (*repository.APIKeyRecord, error) {
	return &repository.APIKeyRecord{ID: "key-1", Key: "api-key"}, nil
}
func (m *mockStore) ListAPIKeys(ctx context.Context, tenantID string, filter repository.APIKeyFilter) ([]repository.APIKeyRecord, error) {
	return nil, nil
}
func (m *mockStore) GetAPIKey(ctx context.Context, tenantID string, idOrKey string) (*repository.APIKeyRecord, error) {
	return nil, nil
}
func (m *mockStore) UpdateAPIKey(ctx context.Context, tenantID string, idOrKey string, params repository.UpdateAPIKeyParams) (*repository.APIKeyRecord, error) {
	return nil, nil
}
func (m *mockStore) RotateAPIKey(ctx context.Context, tenantID string, idOrKey string, params repository.RotateAPIKeyParams) (*repository.APIKeyRecord, error) {
	return nil, nil
}
func (m *mockStore) DeleteAPIKey(ctx context.Context, tenantID string, idOrKey string) error {
	return nil
}
func (m *mockStore) Authenticate(ctx context.Context, key string) (*repository.AuthIdentity, error) {
	return nil, nil
}
func (m *mockStore) TouchAPIKey(ctx context.Context, apiKeyID string, at time.Time) error { return nil }
func (m *mockStore) ConsumeQuota(ctx context.Context, userID string, tokens int) (bool, error) {
	return true, nil
}
func (m *mockStore) ConsumeAPIKeyBudget(ctx context.Context, apiKeyID string, cost float64) (bool, error) {
	return true, nil
}
func (m *mockStore) ConsumeProjectBudget(ctx context.Context, projectID string, cost float64) (bool, error) {
	return true, nil
}
func (m *mockStore) ConsumeTenantBudget(ctx context.Context, tenantID string, cost float64) (bool, error) {
	return true, nil
}
func (m *mockStore) CheckAPIKeyBudget(ctx context.Context, apiKeyID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return &repository.BudgetCheckResult{Allowed: true}, nil
}
func (m *mockStore) CheckProjectBudget(ctx context.Context, projectID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return &repository.BudgetCheckResult{Allowed: true}, nil
}
func (m *mockStore) CheckTenantBudget(ctx context.Context, tenantID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return &repository.BudgetCheckResult{Allowed: true}, nil
}
func (m *mockStore) CheckVirtualKeyBudget(ctx context.Context, virtualKeyID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return &repository.BudgetCheckResult{Allowed: true}, nil
}
func (m *mockStore) ConsumeVirtualKeyBudget(ctx context.Context, virtualKeyID string, cost float64) (bool, error) {
	return true, nil
}
func (m *mockStore) ConsumeBudgets(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID, userID string, cost float64, tokens int) (bool, error) {
	return true, nil
}
func (m *mockStore) ReserveBudgets(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64) (bool, error) {
	return true, nil
}
func (m *mockStore) CommitBudgets(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64) error {
	return nil
}
func (m *mockStore) ReleaseBudgets(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64) error {
	return nil
}
func (m *mockStore) GetBudgetStatus(ctx context.Context, tenantID, projectID, apiKeyID string) ([]repository.BudgetStatus, error) {
	return nil, nil
}
func (m *mockStore) EnsureBootstrapKey(ctx context.Context, params repository.BootstrapAPIKeyParams) error {
	return nil
}
func (m *mockStore) CreateUsageRecord(ctx context.Context, record repository.UsageRecord) error {
	return nil
}
func (m *mockStore) GetUsageSummary(ctx context.Context, tenantID string) (*repository.UsageStats, error) {
	return nil, nil
}
func (m *mockStore) GetProviderUsageSummary(ctx context.Context, tenantID string) (map[string]repository.ProviderUsageStats, error) {
	return nil, nil
}
func (m *mockStore) GetProjectUsageSummary(ctx context.Context, tenantID, projectID string) (*repository.UsageStats, error) {
	return nil, nil
}
func (m *mockStore) GetUserUsageDetail(ctx context.Context, tenantID, userID string, startTime, endTime time.Time) ([]repository.UsageRecord, error) {
	return nil, nil
}
func (m *mockStore) GetUserUsageTrend(ctx context.Context, tenantID, userID string, days int) ([]repository.DailyUsage, error) {
	return nil, nil
}
func (m *mockStore) GetProjectUsageTrend(ctx context.Context, tenantID, projectID string, days int) ([]repository.DailyUsage, error) {
	return nil, nil
}
func (m *mockStore) GetTenantUsageTrend(ctx context.Context, tenantID string, days int) ([]repository.DailyUsage, error) {
	return nil, nil
}
func (m *mockStore) GetUsageSummaryFiltered(ctx context.Context, filter repository.UsageFilter) (*repository.UsageStats, error) {
	return nil, nil
}
func (m *mockStore) GetUsageBreakdown(ctx context.Context, filter repository.UsageFilter, dimension string) ([]repository.UsageBreakdownRow, error) {
	return nil, nil
}
func (m *mockStore) GetUsageTimeBuckets(ctx context.Context, filter repository.UsageFilter, period string, limit int) ([]repository.UsageTimeBucket, error) {
	return nil, nil
}
func (m *mockStore) EnsureTenant(ctx context.Context, params repository.EnsureTenantParams) (*repository.TenantRecord, error) {
	return nil, nil
}
func (m *mockStore) ListTenants(ctx context.Context) ([]repository.TenantRecord, error) {
	return nil, nil
}
func (m *mockStore) GetTenant(ctx context.Context, idOrSlug string) (*repository.TenantRecord, error) {
	return &repository.TenantRecord{ID: "tenant-1", Status: repository.StatusActive}, nil
}
func (m *mockStore) UpdateTenant(ctx context.Context, idOrSlug string, params repository.UpdateTenantParams) (*repository.TenantRecord, error) {
	return nil, nil
}
func (m *mockStore) DeleteTenant(ctx context.Context, idOrSlug string) error { return nil }
func (m *mockStore) ReplaceTenantProviders(ctx context.Context, tenantID string, providers []string) error {
	return nil
}
func (m *mockStore) ListTenantProviders(ctx context.Context, tenantID string) ([]string, error) {
	return nil, nil
}
func (m *mockStore) CreateProject(ctx context.Context, params repository.CreateProjectParams) (*repository.ProjectRecord, error) {
	return &repository.ProjectRecord{ID: "project-1", Status: repository.StatusActive}, nil
}
func (m *mockStore) ListProjects(ctx context.Context, tenantID string) ([]repository.ProjectRecord, error) {
	return nil, nil
}
func (m *mockStore) GetProject(ctx context.Context, tenantID string, idOrSlug string) (*repository.ProjectRecord, error) {
	return &repository.ProjectRecord{ID: "project-1", Status: repository.StatusActive}, nil
}
func (m *mockStore) UpdateProject(ctx context.Context, tenantID string, idOrSlug string, params repository.UpdateProjectParams) (*repository.ProjectRecord, error) {
	return nil, nil
}
func (m *mockStore) DeleteProject(ctx context.Context, tenantID string, idOrSlug string) error {
	return nil
}
func (m *mockStore) UpsertProviderRegistry(ctx context.Context, record repository.ProviderRegistryRecord) error {
	return nil
}
func (m *mockStore) GetProviderRegistry(ctx context.Context, name string) (*repository.ProviderRegistryRecord, error) {
	return nil, nil
}
func (m *mockStore) ListProviderRegistry(ctx context.Context) ([]repository.ProviderRegistryRecord, error) {
	return nil, nil
}
func (m *mockStore) UpdateProviderRegistry(ctx context.Context, name string, params repository.UpdateProviderRegistryParams) (*repository.ProviderRegistryRecord, error) {
	return nil, nil
}
func (m *mockStore) DeleteProviderRegistry(ctx context.Context, name string) error { return nil }
func (m *mockStore) CreateVirtualKey(ctx context.Context, params repository.CreateVirtualKeyParams) (*repository.VirtualKeyRecord, error) {
	return nil, nil
}
func (m *mockStore) ListVirtualKeys(ctx context.Context, tenantID string, filter repository.VirtualKeyFilter) ([]repository.VirtualKeyRecord, error) {
	return nil, nil
}
func (m *mockStore) GetVirtualKey(ctx context.Context, tenantID string, idOrKey string) (*repository.VirtualKeyRecord, error) {
	return nil, nil
}
func (m *mockStore) UpdateVirtualKey(ctx context.Context, tenantID string, idOrKey string, params repository.UpdateVirtualKeyParams) (*repository.VirtualKeyRecord, error) {
	return nil, nil
}
func (m *mockStore) DeleteVirtualKey(ctx context.Context, tenantID string, idOrKey string) error {
	return nil
}
func (m *mockStore) Ping(ctx context.Context) error { return nil }
func (m *mockStore) AuthenticateVirtualKey(ctx context.Context, key string) (*repository.VirtualKeyRecord, error) {
	return nil, nil
}
func (m *mockStore) CreateAuditLog(ctx context.Context, record repository.AuditLogRecord) error {
	return nil
}
func (m *mockStore) ListAuditLogs(ctx context.Context, tenantID string, filter repository.AuditLogFilter) ([]repository.AuditLogRecord, error) {
	return nil, nil

}
func (m *mockStore) DeleteResponsesOlderThan(ctx context.Context, before time.Time) (int64, error) {
	return 0, nil
}

func (m *mockStore) CreateRole(ctx context.Context, params repository.CreateRoleParams) (*repository.RoleRecord, error) {
	return nil, nil
}
func (m *mockStore) ListRoles(ctx context.Context, tenantID string, filter repository.RoleFilter) ([]repository.RoleRecord, error) {
	return nil, nil
}
func (m *mockStore) GetRole(ctx context.Context, tenantID string, id string) (*repository.RoleRecord, error) {
	return nil, nil
}
func (m *mockStore) UpdateRole(ctx context.Context, tenantID string, id string, params repository.UpdateRoleParams) (*repository.RoleRecord, error) {
	return nil, nil
}
func (m *mockStore) DeleteRole(ctx context.Context, tenantID string, id string) error {
	return nil
}
func (m *mockStore) ListPermissions(ctx context.Context) ([]repository.PermissionRecord, error) {
	return nil, nil
}
func (m *mockStore) GetRolePermissions(ctx context.Context, roleID string) ([]repository.PermissionRecord, error) {
	return nil, nil
}
func (m *mockStore) SetRolePermissions(ctx context.Context, roleID string, permissionIDs []string) error {
	return nil
}
func (m *mockStore) GetUserRoleIDs(ctx context.Context, userID string) ([]string, error) {
	return nil, nil
}
func (m *mockStore) GetUserPermissions(ctx context.Context, userID string) ([]repository.PermissionRecord, error) {
	return nil, nil
}

func newMockCatalogService(store repository.Store) *Service {
	authSvc := auth.NewAuth(store)
	return New(&Dependencies{
		Store:     store,
		Auth:      authSvc,
		Limiter:   limiter.NewLimiter(config.LimiterConfig{GlobalQPS: 100000, GlobalTPM: 10000000, GlobalTokenBurst: 1000000, PerUserRequestBurst: 100000, QueueSize: 1000}),
		BudgetSvc: budget.New(store),
	})
}

func TestCreate(t *testing.T) {
	store := &mockStore{
		service: &repository.ServiceRecord{
			ID:                 "svc-1",
			TenantID:           "tenant-1",
			RequestPrefix:      "test-prefix",
			DefaultProvider:    "mock-provider",
			DefaultModel:       "mock-model",
			Enabled:            true,
			PublishedVersionID: "ver-1",
			Config: repository.ServiceConfig{
				Surfaces: []string{"responses"},
				Policy: &repository.ServicePolicyConfig{
					Enabled: true,
					Request: &repository.GuardrailRuleSet{
						AllowModels: []string{"mock-model"},
					},
				},
			},
		},
		version: &repository.ServiceVersionRecord{
			ID:        "ver-1",
			ServiceID: "svc-1",
			Status:    "published",
			Snapshot: repository.ServiceSnapshot{
				RequestPrefix:   "test-prefix",
				DefaultProvider: "mock-provider",
				DefaultModel:    "mock-model",
				Config: repository.ServiceConfig{
					Surfaces: []string{"responses"},
					Policy: &repository.ServicePolicyConfig{
						Enabled: true,
						Request: &repository.GuardrailRuleSet{
							AllowModels: []string{"mock-model"},
							BlockTerms:  []string{"forbidden"},
						},
					},
				},
			},
		},
	}
	svc := newMockCatalogService(store)
	identity := &repository.AuthIdentity{
		TenantID:         "tenant-1",
		APIKeyID:         "key-1",
		APIKeyServices:   []string{"test-prefix"},
		APIKeyModels:     []string{"mock-model"},
		Quota:            -1,
		ProjectBudgetUSD: -1,
		TenantBudgetUSD:  -1,
	}

	// Valid request: prepareRuntimeRequest should succeed and set model/provider
	runtime, prepared, err := svc.prepareRuntimeRequest(context.Background(), identity, "test-prefix", "responses", &provider.ResponseRequest{
		Model:    "mock-model",
		Messages: []provider.Message{{Role: "user", Content: provider.TextBlocks("hi")}},
	})
	if err != nil {
		t.Fatalf("prepareRuntimeRequest() error: %v", err)
	}
	if runtime == nil || runtime.snapshot.DefaultModel != "mock-model" {
		t.Fatalf("expected runtime with mock-model")
	}
	if prepared.Model != "mock-model" || prepared.PreferredProvider != "mock-provider" {
		t.Fatalf("expected prepared request to have model and provider set")
	}

	// Policy violation: blocked term in input
	_, _, err = svc.prepareRuntimeRequest(context.Background(), identity, "test-prefix", "responses", &provider.ResponseRequest{
		Model:    "mock-model",
		Messages: []provider.Message{{Role: "user", Content: provider.TextBlocks("forbidden word")}},
	})
	if !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("expected ErrPolicyViolation for blocked term, got %v", err)
	}
}

func TestCreateStream(t *testing.T) {
	store := &mockStore{
		service: &repository.ServiceRecord{
			ID:                 "svc-1",
			TenantID:           "tenant-1",
			RequestPrefix:      "stream-prefix",
			DefaultProvider:    "mock-provider",
			DefaultModel:       "mock-model",
			Enabled:            true,
			PublishedVersionID: "ver-1",
			Config: repository.ServiceConfig{
				Surfaces: []string{"responses"},
			},
		},
		version: &repository.ServiceVersionRecord{
			ID:        "ver-1",
			ServiceID: "svc-1",
			Status:    "published",
			Snapshot: repository.ServiceSnapshot{
				RequestPrefix:   "stream-prefix",
				DefaultProvider: "mock-provider",
				DefaultModel:    "mock-model",
				Config: repository.ServiceConfig{
					Surfaces: []string{"responses"},
				},
			},
		},
	}
	svc := newMockCatalogService(store)
	identity := &repository.AuthIdentity{
		TenantID:       "tenant-1",
		APIKeyID:       "key-1",
		APIKeyServices: []string{"stream-prefix"},
		APIKeyModels:   []string{"mock-model"},
		Quota:          -1,
	}

	// Valid stream request preparation
	runtime, prepared, err := svc.prepareRuntimeRequest(context.Background(), identity, "stream-prefix", "responses", &provider.ResponseRequest{
		Model:    "mock-model",
		Messages: []provider.Message{{Role: "user", Content: provider.TextBlocks("hi")}},
	})
	if err != nil {
		t.Fatalf("prepareRuntimeRequest() error: %v", err)
	}
	if runtime == nil || prepared == nil {
		t.Fatalf("expected runtime and prepared request")
	}
	if prepared.PreferredProvider != "mock-provider" {
		t.Fatalf("expected preferred provider mock-provider, got %s", prepared.PreferredProvider)
	}
}

func TestCreatePromptInvocation(t *testing.T) {
	store := &mockStore{
		service: &repository.ServiceRecord{
			ID:                 "svc-1",
			TenantID:           "tenant-1",
			RequestPrefix:      "prompt-prefix",
			DefaultProvider:    "mock-provider",
			DefaultModel:       "mock-model",
			Enabled:            true,
			PublishedVersionID: "ver-1",
			Config: repository.ServiceConfig{
				Surfaces: []string{"invoke"},
				PromptTemplate: &repository.PromptTemplateConfig{
					UserTemplate: "Hello {{name}}",
					Variables:    []repository.PromptTemplateVariable{{Name: "name", Required: true}},
				},
			},
		},
		version: &repository.ServiceVersionRecord{
			ID:        "ver-1",
			ServiceID: "svc-1",
			Status:    "published",
			Snapshot: repository.ServiceSnapshot{
				RequestPrefix:   "prompt-prefix",
				DefaultProvider: "mock-provider",
				DefaultModel:    "mock-model",
				Config: repository.ServiceConfig{
					Surfaces: []string{"invoke"},
					PromptTemplate: &repository.PromptTemplateConfig{
						UserTemplate: "Hello {{name}}",
						Variables:    []repository.PromptTemplateVariable{{Name: "name", Required: true}},
					},
				},
			},
		},
	}
	svc := newMockCatalogService(store)
	identity := &repository.AuthIdentity{
		TenantID:       "tenant-1",
		APIKeyID:       "key-1",
		APIKeyServices: []string{"prompt-prefix"},
		APIKeyModels:   []string{"mock-model"},
		Quota:          -1,
	}

	// Missing required variable should fail
	_, _, err := svc.preparePromptRequest(context.Background(), identity, "prompt-prefix", PromptInvokeRequest{
		Variables: map[string]any{},
	})
	if !errors.Is(err, ErrPromptVariableMissing) {
		t.Fatalf("expected ErrPromptVariableMissing, got %v", err)
	}

	// Valid prompt request
	runtime, prepared, err := svc.preparePromptRequest(context.Background(), identity, "prompt-prefix", PromptInvokeRequest{
		Variables: map[string]any{"name": "Gateyes"},
	})
	if err != nil {
		t.Fatalf("preparePromptRequest() error: %v", err)
	}
	if runtime == nil || prepared == nil {
		t.Fatalf("expected runtime and prepared request")
	}
	if prepared.Model != "mock-model" {
		t.Fatalf("expected model mock-model, got %s", prepared.Model)
	}
	if prepared.Messages[0].Content[0].Text != "Hello Gateyes" {
		t.Fatalf("expected rendered prompt, got %s", prepared.Messages[0].Content[0].Text)
	}
}

func TestPrecheck(t *testing.T) {
	store := &mockStore{
		service: &repository.ServiceRecord{
			ID:                 "svc-1",
			TenantID:           "tenant-1",
			RequestPrefix:      "precheck-prefix",
			DefaultProvider:    "mock-provider",
			DefaultModel:       "mock-model",
			Enabled:            true,
			PublishedVersionID: "ver-1",
			Config: repository.ServiceConfig{
				Surfaces: []string{"responses"},
			},
		},
		version: &repository.ServiceVersionRecord{
			ID:        "ver-1",
			ServiceID: "svc-1",
			Status:    "published",
			Snapshot: repository.ServiceSnapshot{
				RequestPrefix:   "precheck-prefix",
				DefaultProvider: "mock-provider",
				DefaultModel:    "mock-model",
				Config: repository.ServiceConfig{
					Surfaces: []string{"responses"},
				},
			},
		},
	}
	svc := newMockCatalogService(store)

	// Missing service access
	identityNoAccess := &repository.AuthIdentity{
		TenantID:       "tenant-1",
		APIKeyID:       "key-1",
		APIKeyServices: []string{"other-prefix"},
		APIKeyModels:   []string{"mock-model"},
		Quota:          -1,
	}
	_, _, err := svc.prepareRuntimeRequest(context.Background(), identityNoAccess, "precheck-prefix", "responses", &provider.ResponseRequest{
		Model:    "mock-model",
		Messages: []provider.Message{{Role: "user", Content: provider.TextBlocks("hi")}},
	})
	if !errors.Is(err, ErrServiceAccessDenied) {
		t.Fatalf("expected ErrServiceAccessDenied, got %v", err)
	}

	// Missing model access
	identityNoModel := &repository.AuthIdentity{
		TenantID:       "tenant-1",
		APIKeyID:       "key-1",
		APIKeyServices: []string{"precheck-prefix"},
		APIKeyModels:   []string{"other-model"},
		Quota:          -1,
	}
	_, _, err = svc.prepareRuntimeRequest(context.Background(), identityNoModel, "precheck-prefix", "responses", &provider.ResponseRequest{
		Model:    "mock-model",
		Messages: []provider.Message{{Role: "user", Content: provider.TextBlocks("hi")}},
	})
	if !errors.Is(err, auth.ErrModelNotAllowed) {
		t.Fatalf("expected ErrModelNotAllowed, got %v", err)
	}
}

func TestApplyRequestPolicy(t *testing.T) {
	svc := New(&Dependencies{})
	policy := &repository.ServicePolicyConfig{
		Enabled: true,
		Request: &repository.GuardrailRuleSet{
			AllowModels:   []string{"allowed-model"},
			BlockModels:   []string{"blocked-model"},
			BlockTerms:    []string{"forbidden"},
			MaxInputChars: 10,
			RedactTerms:   []string{"secret"},
		},
	}

	// Allowed model passes
	req1 := &provider.ResponseRequest{Model: "allowed-model", Messages: []provider.Message{{Role: "user", Content: provider.TextBlocks("hi")}}}
	if err := svc.applyRequestPolicy(policy, req1); err != nil {
		t.Fatalf("allowed model should pass: %v", err)
	}

	// Blocked model fails
	req2 := &provider.ResponseRequest{Model: "blocked-model"}
	if err := svc.applyRequestPolicy(policy, req2); !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("expected ErrPolicyViolation for blocked model, got %v", err)
	}

	// Model not in allowlist fails
	req3 := &provider.ResponseRequest{Model: "unknown-model"}
	if err := svc.applyRequestPolicy(policy, req3); !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("expected ErrPolicyViolation for unknown model, got %v", err)
	}

	// Blocked term fails
	req4 := &provider.ResponseRequest{Model: "allowed-model", Messages: []provider.Message{{Role: "user", Content: provider.TextBlocks("forbidden word")}}}
	if err := svc.applyRequestPolicy(policy, req4); !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("expected ErrPolicyViolation for blocked term, got %v", err)
	}

	// Max input chars fails
	req5 := &provider.ResponseRequest{Model: "allowed-model", Messages: []provider.Message{{Role: "user", Content: provider.TextBlocks("this is way too long")}}}
	if err := svc.applyRequestPolicy(policy, req5); !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("expected ErrPolicyViolation for long input, got %v", err)
	}

	// Redact terms applied
	req6 := &provider.ResponseRequest{Model: "allowed-model", Messages: []provider.Message{{Role: "user", Content: provider.TextBlocks("my secret")}}}
	if err := svc.applyRequestPolicy(policy, req6); err != nil {
		t.Fatalf("redact should pass: %v", err)
	}
	if req6.InputText() != "my [REDACTED]" {
		t.Fatalf("expected redacted text, got %s", req6.InputText())
	}
}

func TestApplyResponsePolicy(t *testing.T) {
	store := &mockStore{}
	svc := New(&Dependencies{Store: store})
	policy := &repository.ServicePolicyConfig{
		Enabled: true,
		Response: &repository.GuardrailRuleSet{
			BlockTerms:     []string{"blocked-output"},
			MaxOutputChars: 20,
			RedactTerms:    []string{"sensitive"},
		},
	}
	runtime := &serviceRuntime{
		service: &repository.ServiceRecord{ID: "svc-1", TenantID: "tenant-1"},
		snapshot: repository.ServiceSnapshot{
			DefaultProvider: "mock-provider",
			Config: repository.ServiceConfig{
				Policy: policy,
			},
		},
	}
	identity := &repository.AuthIdentity{TenantID: "tenant-1", ProjectID: "project-1"}

	// Blocked term in output fails
	resp1 := &provider.Response{
		ID:     "resp-1",
		Output: []provider.ResponseOutput{{Type: "message", Content: []provider.ResponseContent{{Type: "output_text", Text: "blocked-output"}}}},
	}
	if err := svc.applyResponsePolicy(context.Background(), identity, runtime, resp1); !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("expected ErrPolicyViolation for blocked output, got %v", err)
	}

	// Max output chars fails
	resp2 := &provider.Response{
		ID:     "resp-2",
		Output: []provider.ResponseOutput{{Type: "message", Content: []provider.ResponseContent{{Type: "output_text", Text: "this is a very long output text"}}}},
	}
	if err := svc.applyResponsePolicy(context.Background(), identity, runtime, resp2); !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("expected ErrPolicyViolation for long output, got %v", err)
	}

	// Redact terms applied
	resp3 := &provider.Response{
		ID:     "resp-3",
		Output: []provider.ResponseOutput{{Type: "message", Content: []provider.ResponseContent{{Type: "output_text", Text: "sensitive data"}}}},
	}
	if err := svc.applyResponsePolicy(context.Background(), identity, runtime, resp3); err != nil {
		t.Fatalf("redact should pass: %v", err)
	}
	if resp3.OutputText() != "[REDACTED] data" {
		t.Fatalf("expected redacted output, got %s", resp3.OutputText())
	}
}
