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
	if record.Status != "completed" {
		t.Fatalf("expected completed response, got %q", record.Status)
	}

	createdAt := time.Now().UTC()
	if err := store.CreateUsageRecord(ctx, repository.UsageRecord{
		ID:               "usage-1",
		TenantID:         tenant.ID,
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
