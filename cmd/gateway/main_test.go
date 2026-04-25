package main

import (
	"context"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
)

type fakeIdentityStore struct {
	params []repository.BootstrapAPIKeyParams
	err    error
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

func (f *fakeIdentityStore) EnsureBootstrapKey(ctx context.Context, params repository.BootstrapAPIKeyParams) error {
	if f.err != nil {
		return f.err
	}
	f.params = append(f.params, params)
	return nil
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
