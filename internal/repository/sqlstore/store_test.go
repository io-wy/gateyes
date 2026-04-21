package sqlstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/repository"
)

func TestAuthenticateLoadsIdentityAndModels(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-a",
		Slug:   "tenant-a",
		Name:   "tenant-a",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenant.ID,
		Key:        "test-key",
		SecretHash: repository.HashSecret("test-secret"),
		Name:       "test-admin",
		Email:      "admin@example.com",
		Role:       repository.RoleTenantAdmin,
		Quota:      50,
		QPS:        10,
		Models:     []string{"gpt-test", "claude-test"},
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	identity, err := store.Authenticate(ctx, "test-key")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	if identity.TenantID != tenant.ID {
		t.Fatalf("expected tenant %q, got %q", tenant.ID, identity.TenantID)
	}
	if identity.Role != repository.RoleTenantAdmin {
		t.Fatalf("expected tenant admin role, got %q", identity.Role)
	}
	if len(identity.Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(identity.Models))
	}
}

func TestConsumeQuotaRespectsQuota(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-a",
		Slug:   "tenant-a",
		Name:   "tenant-a",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenant.ID,
		Key:        "quota-key",
		SecretHash: repository.HashSecret("quota-secret"),
		Name:       "quota-user",
		Email:      "quota@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      10,
		QPS:        1,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	identity, err := store.Authenticate(ctx, "quota-key")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	ok, err := store.ConsumeQuota(ctx, identity.UserID, 6)
	if err != nil {
		t.Fatalf("consume quota first call: %v", err)
	}
	if !ok {
		t.Fatalf("expected first consume to succeed")
	}

	ok, err = store.ConsumeQuota(ctx, identity.UserID, 5)
	if err != nil {
		t.Fatalf("consume quota second call: %v", err)
	}
	if ok {
		t.Fatalf("expected second consume to fail because quota is exhausted")
	}
}

func TestReplaceTenantProvidersListsScopedProviders(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-a",
		Slug:   "tenant-a",
		Name:   "tenant-a",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	if err := store.ReplaceTenantProviders(ctx, tenant.ID, []string{"anthropic-primary", "openai-primary"}); err != nil {
		t.Fatalf("replace tenant providers: %v", err)
	}

	providers, err := store.ListTenantProviders(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("list tenant providers: %v", err)
	}
	if len(providers) != 2 || providers[0] != "anthropic-primary" || providers[1] != "openai-primary" {
		t.Fatalf("unexpected provider list: %v", providers)
	}
}

func TestProviderRegistryCRUD(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	record := repository.ProviderRegistryRecord{
		Name:                     "openai-primary",
		Type:                     "openai",
		Vendor:                   "vllm",
		BaseURL:                  "https://api.example.com/v1",
		Endpoint:                 "responses",
		Model:                    "gpt-test",
		Enabled:                  true,
		Drain:                    false,
		HealthStatus:             "healthy",
		RoutingWeight:            5,
		SupportsChat:             true,
		SupportsResponses:        true,
		SupportsMessages:         false,
		SupportsStream:           true,
		SupportsTools:            true,
		SupportsImages:           true,
		SupportsStructuredOutput: true,
		SupportsLongContext:      true,
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	if err := store.UpsertProviderRegistry(ctx, record); err != nil {
		t.Fatalf("UpsertProviderRegistry(create) error: %v", err)
	}

	got, err := store.GetProviderRegistry(ctx, "openai-primary")
	if err != nil {
		t.Fatalf("GetProviderRegistry() error: %v", err)
	}
	if got.Name != "openai-primary" || got.RoutingWeight != 5 || !got.SupportsResponses {
		t.Fatalf("GetProviderRegistry() = %+v, want stored record", got)
	}

	all, err := store.ListProviderRegistry(ctx)
	if err != nil {
		t.Fatalf("ListProviderRegistry() error: %v", err)
	}
	if len(all) != 1 || all[0].Name != "openai-primary" {
		t.Fatalf("ListProviderRegistry() = %+v, want one openai-primary record", all)
	}

	drain := true
	health := "unhealthy"
	weight := 9
	stream := false
	updated, err := store.UpdateProviderRegistry(ctx, "openai-primary", repository.UpdateProviderRegistryParams{
		Drain:         &drain,
		HealthStatus:  &health,
		RoutingWeight: &weight,
		SupportsStream: &stream,
	})
	if err != nil {
		t.Fatalf("UpdateProviderRegistry() error: %v", err)
	}
	if !updated.Drain || updated.HealthStatus != "unhealthy" || updated.RoutingWeight != 9 || updated.SupportsStream {
		t.Fatalf("UpdateProviderRegistry() = %+v, want updated runtime fields", updated)
	}

	record.Drain = false
	record.HealthStatus = "healthy"
	record.RoutingWeight = 3
	if err := store.UpsertProviderRegistry(ctx, record); err != nil {
		t.Fatalf("UpsertProviderRegistry(update) error: %v", err)
	}
	got, err = store.GetProviderRegistry(ctx, "openai-primary")
	if err != nil {
		t.Fatalf("GetProviderRegistry(after upsert update) error: %v", err)
	}
	if got.RoutingWeight != 3 || got.HealthStatus != "healthy" {
		t.Fatalf("GetProviderRegistry(after upsert update) = %+v, want refreshed config-backed values", got)
	}
}

func TestProjectCRUDAndProjectAwareIdentity(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-project",
		Slug:   "tenant-project",
		Name:   "Tenant Project",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}

	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID:  tenant.ID,
		Slug:      "proj-a",
		Name:      "Project A",
		Status:    repository.StatusActive,
		BudgetUSD: 50,
	})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if project.TenantID != tenant.ID || project.BudgetUSD != 50 {
		t.Fatalf("CreateProject() = %+v, want tenant binding and budget", project)
	}

	projects, err := store.ListProjects(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListProjects() error: %v", err)
	}
	if len(projects) != 1 || projects[0].Slug != "proj-a" {
		t.Fatalf("ListProjects() = %+v, want one project", projects)
	}

	gotProject, err := store.GetProject(ctx, tenant.ID, "proj-a")
	if err != nil {
		t.Fatalf("GetProject(by slug) error: %v", err)
	}
	if gotProject.ID != project.ID {
		t.Fatalf("GetProject(by slug) = %+v, want same project", gotProject)
	}

	name := "Project A Updated"
	budget := 80.0
	updatedProject, err := store.UpdateProject(ctx, tenant.ID, project.ID, repository.UpdateProjectParams{
		Name:      &name,
		BudgetUSD: &budget,
	})
	if err != nil {
		t.Fatalf("UpdateProject() error: %v", err)
	}
	if updatedProject.Name != name || updatedProject.BudgetUSD != 80 {
		t.Fatalf("UpdateProject() = %+v, want updated project fields", updatedProject)
	}

	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:     tenant.ID,
		ProjectID:    project.ID,
		Key:          "project-key",
		SecretHash:   repository.HashSecret("project-secret"),
		Name:         "project-user",
		Email:        "project@example.com",
		Role:         repository.RoleTenantUser,
		Quota:        100,
		QPS:          10,
		KeyBudgetUSD: 12.5,
		Models:       []string{"gpt-test"},
	}); err != nil {
		t.Fatalf("EnsureBootstrapKey(project) error: %v", err)
	}

	identity, err := store.Authenticate(ctx, "project-key")
	if err != nil {
		t.Fatalf("Authenticate(project-key) error: %v", err)
	}
	if identity.ProjectID != project.ID || identity.ProjectSlug != "proj-a" || identity.ProjectBudgetUSD != 80 || identity.APIKeyBudgetUSD != 12.5 {
		t.Fatalf("Authenticate(project-key) = %+v, want project-aware identity", identity)
	}

	if ok, err := store.ConsumeAPIKeyBudget(ctx, identity.APIKeyID, 2.5); err != nil || !ok {
		t.Fatalf("ConsumeAPIKeyBudget() = (%v,%v), want (true,nil)", ok, err)
	}
	if ok, err := store.ConsumeProjectBudget(ctx, identity.ProjectID, 10); err != nil || !ok {
		t.Fatalf("ConsumeProjectBudget() = (%v,%v), want (true,nil)", ok, err)
	}
}

func TestResponsesAndUsagePersistence(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-a",
		Slug:   "tenant-a",
		Name:   "tenant-a",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenant.ID,
		Key:        "resp-key",
		SecretHash: repository.HashSecret("resp-secret"),
		Name:       "resp-user",
		Email:      "resp@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      100,
		QPS:        10,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	identity, err := store.Authenticate(ctx, "resp-key")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	if err := store.CreateResponse(ctx, repository.ResponseRecord{
		ID:           "resp-1",
		TenantID:     tenant.ID,
		ProjectID:    identity.ProjectID,
		UserID:       identity.UserID,
		APIKeyID:     identity.APIKeyID,
		ProviderName: "openai-primary",
		Model:        "gpt-test",
		Status:       "in_progress",
		RequestBody:  []byte(`{"input":"hello"}`),
	}); err != nil {
		t.Fatalf("create response: %v", err)
	}
	if err := store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:           "resp-1",
		TenantID:     tenant.ID,
		ProviderName: "openai-primary",
		Model:        "gpt-test",
		Status:       "completed",
		ResponseBody: []byte(`{"output":"hello"}`),
	}); err != nil {
		t.Fatalf("update response: %v", err)
	}

	record, err := store.GetResponse(ctx, tenant.ID, "resp-1")
	if err != nil {
		t.Fatalf("get response: %v", err)
	}
	if record.Status != "completed" || record.ProjectID != identity.ProjectID {
		t.Fatalf("expected completed response with project binding, got %+v", record)
	}

	createdAt := time.Now().UTC()
	if err := store.CreateUsageRecord(ctx, repository.UsageRecord{
		ID:               "usage-1",
		TenantID:         tenant.ID,
		ProjectID:        identity.ProjectID,
		UserID:           identity.UserID,
		APIKeyID:         identity.APIKeyID,
		ProviderName:     "openai-primary",
		Model:            "gpt-test",
		PromptTokens:     3,
		CompletionTokens: 2,
		TotalTokens:      5,
		Cost:             1.25,
		LatencyMs:        42,
		Status:           "success",
		CreatedAt:        createdAt,
	}); err != nil {
		t.Fatalf("create usage record: %v", err)
	}

	summary, err := store.GetUsageSummary(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("usage summary: %v", err)
	}
	if summary.SuccessRequests != 1 || summary.TotalTokens != 5 {
		t.Fatalf("unexpected usage summary: %+v", summary)
	}

	byProvider, err := store.GetProviderUsageSummary(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("provider usage summary: %v", err)
	}
	stats := byProvider["openai-primary"]
	if stats.SuccessRequests != 1 || stats.TotalTokens != 5 {
		t.Fatalf("unexpected provider usage summary: %+v", stats)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	database, err := db.Open(config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "sqlstore.db"),
		AutoMigrate:            true,
		MaxOpenConns:           4,
		MaxIdleConns:           4,
		ConnMaxLifetimeSeconds: 60,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	return New(database)
}
