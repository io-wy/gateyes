package main

import (
	"context"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
)

type fakeIdentityStore struct {
	params           []repository.BootstrapAPIKeyParams
	providerRegistry map[string]repository.ProviderRegistryRecord
	err              error
}

func (f *fakeIdentityStore) Authenticate(ctx context.Context, key string) (*repository.AuthIdentity, error) {
	return nil, repository.ErrNotFound
}

func (f *fakeIdentityStore) TouchAPIKey(ctx context.Context, apiKeyID string, at time.Time) error {
	return nil
}

func (f *fakeIdentityStore) ConsumeQuota(ctx context.Context, userID string, tokens int) (bool, error) {
	return true, nil
}

func (f *fakeIdentityStore) ConsumeAPIKeyBudget(ctx context.Context, apiKeyID string, cost float64) (bool, error) {
	return true, nil
}

func (f *fakeIdentityStore) ConsumeProjectBudget(ctx context.Context, projectID string, cost float64) (bool, error) {
	return true, nil
}

func (f *fakeIdentityStore) ConsumeTenantBudget(ctx context.Context, tenantID string, cost float64) (bool, error) {
	return true, nil
}

func (f *fakeIdentityStore) CheckAPIKeyBudget(ctx context.Context, apiKeyID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return &repository.BudgetCheckResult{Allowed: true, Scope: "api_key"}, nil
}

func (f *fakeIdentityStore) CheckProjectBudget(ctx context.Context, projectID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return &repository.BudgetCheckResult{Allowed: true, Scope: "project"}, nil
}

func (f *fakeIdentityStore) CheckTenantBudget(ctx context.Context, tenantID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return &repository.BudgetCheckResult{Allowed: true, Scope: "tenant"}, nil
}

func (f *fakeIdentityStore) GetBudgetStatus(ctx context.Context, tenantID, projectID, apiKeyID string) ([]repository.BudgetStatus, error) {
	return nil, nil
}

func (f *fakeIdentityStore) CheckVirtualKeyBudget(ctx context.Context, virtualKeyID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return &repository.BudgetCheckResult{Allowed: true}, nil
}
func (f *fakeIdentityStore) ConsumeVirtualKeyBudget(ctx context.Context, virtualKeyID string, cost float64) (bool, error) {
	return true, nil
}
func (f *fakeIdentityStore) ConsumeBudgets(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID, userID string, cost float64, tokens int) (bool, error) {
	return true, nil
}
func (f *fakeIdentityStore) ReserveBudgets(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64) (bool, error) {
	return true, nil
}
func (f *fakeIdentityStore) CommitBudgets(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64) error {
	return nil
}
func (f *fakeIdentityStore) ReleaseBudgets(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64) error {
	return nil
}
func (f *fakeIdentityStore) EnsureBootstrapKey(ctx context.Context, params repository.BootstrapAPIKeyParams) error {
	if f.err != nil {
		return f.err
	}
	f.params = append(f.params, params)
	return nil
}
func (f *fakeIdentityStore) EnsureTenant(ctx context.Context, params repository.EnsureTenantParams) (*repository.TenantRecord, error) {
	return &repository.TenantRecord{ID: params.ID}, nil
}
func (f *fakeIdentityStore) ListTenants(ctx context.Context) ([]repository.TenantRecord, error) {
	return nil, nil
}
func (f *fakeIdentityStore) GetTenant(ctx context.Context, idOrSlug string) (*repository.TenantRecord, error) {
	return &repository.TenantRecord{ID: idOrSlug}, nil
}
func (f *fakeIdentityStore) UpdateTenant(ctx context.Context, idOrSlug string, params repository.UpdateTenantParams) (*repository.TenantRecord, error) {
	return nil, nil
}
func (f *fakeIdentityStore) DeleteTenant(ctx context.Context, idOrSlug string) error { return nil }
func (f *fakeIdentityStore) ListTenantProviders(ctx context.Context, tenantID string) ([]string, error) {
	return nil, nil
}
func (f *fakeIdentityStore) ReplaceTenantProviders(ctx context.Context, tenantID string, providerNames []string) error {
	return nil
}
func (f *fakeIdentityStore) ListProviderRegistry(ctx context.Context) ([]repository.ProviderRegistryRecord, error) {
	items := make([]repository.ProviderRegistryRecord, 0, len(f.providerRegistry))
	for _, item := range f.providerRegistry {
		items = append(items, item)
	}
	return items, nil
}
func (f *fakeIdentityStore) GetProviderRegistry(ctx context.Context, name string) (*repository.ProviderRegistryRecord, error) {
	if f.providerRegistry == nil {
		return nil, repository.ErrNotFound
	}
	record, ok := f.providerRegistry[name]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &record, nil
}
func (f *fakeIdentityStore) UpsertProviderRegistry(ctx context.Context, record repository.ProviderRegistryRecord) error {
	if f.providerRegistry == nil {
		f.providerRegistry = make(map[string]repository.ProviderRegistryRecord)
	}
	f.providerRegistry[record.Name] = record
	return nil
}
func (f *fakeIdentityStore) UpdateProviderRegistry(ctx context.Context, name string, params repository.UpdateProviderRegistryParams) (*repository.ProviderRegistryRecord, error) {
	return nil, nil
}
func (f *fakeIdentityStore) DeleteProviderRegistry(ctx context.Context, name string) error {
	return nil
}

func TestSeedTenantProviders(t *testing.T) {
	store := &fakeIdentityStore{}
	ctx := context.Background()
	if err := seedTenantProviders(ctx, store, "tenant-a", []string{"p1", "p2"}); err != nil {
		t.Fatalf("seedTenantProviders() error: %v", err)
	}
	if len(store.params) != 0 {
		t.Fatalf("seedTenantProviders should not call EnsureBootstrapKey, got %d calls", len(store.params))
	}
}

func TestSeedProviderRegistry(t *testing.T) {
	store := &fakeIdentityStore{
		providerRegistry: map[string]repository.ProviderRegistryRecord{
			"p1": {
				Name:               "p1",
				SupportsEmbeddings: true,
				RuntimeConfig:      &repository.ProviderRuntimeConfig{APIKey: "existing-key"},
				CreatedAt:          time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
			},
		},
	}
	ctx := context.Background()
	providers := []config.ProviderConfig{
		{Name: "p1", Type: "openai", Endpoint: "responses", Enabled: true},
		{Name: "p2", Type: "anthropic", Enabled: false},
	}
	if err := seedProviderRegistry(ctx, store, providers); err != nil {
		t.Fatalf("seedProviderRegistry() error: %v", err)
	}
	p1 := store.providerRegistry["p1"]
	if p1.SupportsEmbeddings {
		t.Fatalf("seedProviderRegistry() kept stale SupportsEmbeddings=true for p1")
	}
	if p1.RuntimeConfig == nil || p1.RuntimeConfig.APIKey != "existing-key" {
		t.Fatalf("seedProviderRegistry() RuntimeConfig = %+v, want existing runtime config preserved", p1.RuntimeConfig)
	}
	if p1.CreatedAt.IsZero() {
		t.Fatalf("seedProviderRegistry() should preserve existing CreatedAt")
	}
	if _, ok := store.providerRegistry["p2"]; !ok {
		t.Fatalf("seedProviderRegistry() did not insert p2")
	}
}

func TestBuildGuardrails(t *testing.T) {
	if buildGuardrails(nil) != nil {
		t.Fatal("buildGuardrails(nil) should return nil")
	}
	if buildGuardrails([]config.GuardrailConfig{}) != nil {
		t.Fatal("buildGuardrails(empty) should return nil")
	}
	if buildGuardrails([]config.GuardrailConfig{{Type: "unknown", Name: "x"}}) != nil {
		t.Fatal("buildGuardrails(unknown type) should return nil")
	}
	m := buildGuardrails([]config.GuardrailConfig{
		{Name: "blocklist", Type: "regex", RequestPatterns: []string{`bad`}},
	})
	if m == nil {
		t.Fatal("buildGuardrails(regex) should return non-nil")
	}
}

func TestEnabledProviderNamesAndSeedHelpers(t *testing.T) {
	if got := enabledProviderNames([]config.ProviderConfig{
		{Name: "openai-a", Enabled: true},
		{Name: "openai-b", Enabled: false},
		{Name: "anthropic-a", Enabled: true},
	}); len(got) != 2 || got[0] != "openai-a" || got[1] != "anthropic-a" {
		t.Fatalf("enabledProviderNames() = %v, want [openai-a anthropic-a]", got)
	}

	store := &fakeIdentityStore{}
	err := seedConfiguredAPIKeys(context.Background(), store, "tenant-a", []config.APIKeyConfig{{
		Key:    "key-1",
		Secret: "secret-1",
		Quota:  100,
		QPS:    3,
		Models: []string{"gpt-1"},
	}})
	if err != nil {
		t.Fatalf("seedConfiguredAPIKeys() error: %v", err)
	}
	if len(store.params) != 1 || store.params[0].Role != repository.RoleTenantUser {
		t.Fatalf("seedConfiguredAPIKeys() params = %+v, want tenant user bootstrap key", store.params)
	}

	store.params = nil
	if err := seedBootstrapAdmin(context.Background(), store, "tenant-a", config.AdminConfig{
		BootstrapKey:    "admin-key",
		BootstrapSecret: "admin-secret",
	}); err != nil {
		t.Fatalf("seedBootstrapAdmin() error: %v", err)
	}
	if len(store.params) != 1 || store.params[0].Role != repository.RoleSuperAdmin {
		t.Fatalf("seedBootstrapAdmin() params = %+v, want super admin bootstrap key", store.params)
	}

	store.params = nil
	if err := seedBootstrapAdmin(context.Background(), store, "tenant-a", config.AdminConfig{}); err != nil {
		t.Fatalf("seedBootstrapAdmin(empty) error: %v", err)
	}
	if len(store.params) != 0 {
		t.Fatalf("seedBootstrapAdmin(empty) calls = %d, want %d", len(store.params), 0)
	}
}
