package sqlstore

import (
	"context"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

func seedVirtualKeyTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID: "vk-tenant", Slug: "vk-tenant", Name: "vk-tenant", Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID: tenant.ID, Key: "vk-parent-key", SecretHash: repository.HashSecret("vk-parent-secret"),
		Name: "vk-parent", Role: repository.RoleTenantUser, Quota: 1000,
	}); err != nil {
		t.Fatalf("seed parent key: %v", err)
	}
	return store, tenant.ID
}

func TestCreateAndGetVirtualKey(t *testing.T) {
	store, tenantID := seedVirtualKeyTestStore(t)
	ctx := context.Background()

	record, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID:         tenantID,
		UserID:           "user-1",
		APIKeyID:         "vk-parent-key",
		ProjectID:        "proj-1",
		Name:             "test-vk",
		Key:              "vk-test-001",
		SecretHash:       repository.HashSecret("vs-test-001"),
		BudgetUSD:        50.0,
		BudgetPolicy:     repository.BudgetPolicyHardReject,
		RateLimitQPS:     30,
		AllowedModels:    []string{"gpt-4", "claude-3"},
		AllowedProviders: []string{"openai"},
		CallbackURL:      "https://example.com/cb",
	})
	if err != nil {
		t.Fatalf("CreateVirtualKey: %v", err)
	}
	if record.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if record.Key != "vk-test-001" {
		t.Fatalf("Key = %q, want vk-test-001", record.Key)
	}
	if record.Status != repository.StatusActive {
		t.Fatalf("Status = %q, want active", record.Status)
	}
	if len(record.AllowedModels) != 2 {
		t.Fatalf("AllowedModels = %v, want 2 items", record.AllowedModels)
	}
	if record.CallbackURL != "https://example.com/cb" {
		t.Fatalf("CallbackURL = %q", record.CallbackURL)
	}

	got, err := store.GetVirtualKey(ctx, tenantID, record.ID)
	if err != nil {
		t.Fatalf("GetVirtualKey: %v", err)
	}
	if got.Name != "test-vk" {
		t.Fatalf("Name = %q, want test-vk", got.Name)
	}
}

func TestGetVirtualKeyByKey(t *testing.T) {
	store, tenantID := seedVirtualKeyTestStore(t)
	ctx := context.Background()

	_, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID: tenantID, UserID: "user-1", APIKeyID: "vk-parent-key",
		Name: "key-lookup", Key: "vk-lookup", SecretHash: repository.HashSecret("vs-lookup"),
	})
	if err != nil {
		t.Fatalf("CreateVirtualKey: %v", err)
	}

	got, err := store.GetVirtualKey(ctx, tenantID, "vk-lookup")
	if err != nil {
		t.Fatalf("GetVirtualKey(by key): %v", err)
	}
	if got.Key != "vk-lookup" {
		t.Fatalf("Key = %q, want vk-lookup", got.Key)
	}
}

func TestListVirtualKeys(t *testing.T) {
	store, tenantID := seedVirtualKeyTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"vk-a", "vk-b", "vk-c"} {
		if _, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
			TenantID: tenantID, UserID: "user-1", APIKeyID: "vk-parent-key",
			Name: name, Key: name, SecretHash: repository.HashSecret("vs-" + name),
		}); err != nil {
			t.Fatalf("CreateVirtualKey(%s): %v", name, err)
		}
	}

	items, err := store.ListVirtualKeys(ctx, tenantID, repository.VirtualKeyFilter{})
	if err != nil {
		t.Fatalf("ListVirtualKeys: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("ListVirtualKeys count = %d, want 3", len(items))
	}

	filtered, err := store.ListVirtualKeys(ctx, tenantID, repository.VirtualKeyFilter{Status: repository.StatusActive})
	if err != nil {
		t.Fatalf("ListVirtualKeys(filtered): %v", err)
	}
	if len(filtered) != 3 {
		t.Fatalf("ListVirtualKeys(active) count = %d, want 3", len(filtered))
	}
}

func TestUpdateVirtualKey(t *testing.T) {
	store, tenantID := seedVirtualKeyTestStore(t)
	ctx := context.Background()

	record, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID: tenantID, UserID: "user-1", APIKeyID: "vk-parent-key",
		Name: "before", Key: "vk-upd", SecretHash: repository.HashSecret("vs-upd"),
		BudgetUSD: 10.0,
	})
	if err != nil {
		t.Fatalf("CreateVirtualKey: %v", err)
	}

	newName := "after"
	newBudget := 100.0
	updated, err := store.UpdateVirtualKey(ctx, tenantID, record.ID, repository.UpdateVirtualKeyParams{
		Name:      &newName,
		BudgetUSD: &newBudget,
	})
	if err != nil {
		t.Fatalf("UpdateVirtualKey: %v", err)
	}
	if updated.Name != "after" {
		t.Fatalf("Name = %q, want after", updated.Name)
	}
	if updated.BudgetUSD != 100.0 {
		t.Fatalf("BudgetUSD = %f, want 100.0", updated.BudgetUSD)
	}
}

func TestDeleteVirtualKey(t *testing.T) {
	store, tenantID := seedVirtualKeyTestStore(t)
	ctx := context.Background()

	record, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID: tenantID, UserID: "user-1", APIKeyID: "vk-parent-key",
		Name: "to-delete", Key: "vk-del", SecretHash: repository.HashSecret("vs-del"),
	})
	if err != nil {
		t.Fatalf("CreateVirtualKey: %v", err)
	}

	if err := store.DeleteVirtualKey(ctx, tenantID, record.ID); err != nil {
		t.Fatalf("DeleteVirtualKey: %v", err)
	}
	if _, err := store.GetVirtualKey(ctx, tenantID, record.ID); err != repository.ErrNotFound {
		t.Fatalf("GetVirtualKey after delete: %v, want ErrNotFound", err)
	}
}

func TestAuthenticateVirtualKey(t *testing.T) {
	store, tenantID := seedVirtualKeyTestStore(t)
	ctx := context.Background()

	_, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID: tenantID, UserID: "user-1", APIKeyID: "vk-parent-key",
		Name: "auth-test", Key: "vk-auth", SecretHash: repository.HashSecret("vs-auth"),
		BudgetUSD: 50.0, BudgetPolicy: repository.BudgetPolicyHardReject, RateLimitQPS: 20,
		AllowedModels: []string{"gpt-4"}, AllowedProviders: []string{"openai"},
	})
	if err != nil {
		t.Fatalf("CreateVirtualKey: %v", err)
	}

	vk, err := store.AuthenticateVirtualKey(ctx, "vk-auth")
	if err != nil {
		t.Fatalf("AuthenticateVirtualKey: %v", err)
	}
	if vk.UserID != "user-1" {
		t.Fatalf("UserID = %q, want user-1", vk.UserID)
	}
	if vk.BudgetUSD != 50.0 {
		t.Fatalf("BudgetUSD = %f, want 50.0", vk.BudgetUSD)
	}
	if len(vk.AllowedModels) != 1 || vk.AllowedModels[0] != "gpt-4" {
		t.Fatalf("AllowedModels = %v, want [gpt-4]", vk.AllowedModels)
	}

	_, err = store.AuthenticateVirtualKey(ctx, "vk-nonexistent")
	if err != repository.ErrNotFound {
		t.Fatalf("AuthenticateVirtualKey(missing): %v, want ErrNotFound", err)
	}
}

func TestAuthenticateVirtualKeyExpired(t *testing.T) {
	store, tenantID := seedVirtualKeyTestStore(t)
	ctx := context.Background()

	past := time.Now().Add(-1 * time.Hour)
	_, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID: tenantID, UserID: "user-1", APIKeyID: "vk-parent-key",
		Name: "expired", Key: "vk-exp", SecretHash: repository.HashSecret("vs-exp"),
		ExpiresAt: &past,
	})
	if err != nil {
		t.Fatalf("CreateVirtualKey: %v", err)
	}

	_, err = store.AuthenticateVirtualKey(ctx, "vk-exp")
	if err != repository.ErrNotFound {
		t.Fatalf("AuthenticateVirtualKey(expired): %v, want ErrNotFound", err)
	}
}

func TestConsumeVirtualKeyBudget(t *testing.T) {
	store, tenantID := seedVirtualKeyTestStore(t)
	ctx := context.Background()

	record, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID: tenantID, UserID: "user-1", APIKeyID: "vk-parent-key",
		Name: "budget", Key: "vk-budget", SecretHash: repository.HashSecret("vs-budget"),
		BudgetUSD: 10.0, BudgetPolicy: repository.BudgetPolicyHardReject,
	})
	if err != nil {
		t.Fatalf("CreateVirtualKey: %v", err)
	}

	ok, err := store.ConsumeVirtualKeyBudget(ctx, record.ID, 5.0)
	if err != nil {
		t.Fatalf("ConsumeVirtualKeyBudget: %v", err)
	}
	if !ok {
		t.Fatal("should allow within budget")
	}

	ok, err = store.ConsumeVirtualKeyBudget(ctx, record.ID, 6.0)
	if err != nil {
		t.Fatalf("ConsumeVirtualKeyBudget: %v", err)
	}
	if ok {
		t.Fatal("should deny over budget")
	}

	got, _ := store.GetVirtualKey(ctx, tenantID, record.ID)
	if got.SpentUSD != 5.0 {
		t.Fatalf("SpentUSD = %f, want 5.0", got.SpentUSD)
	}
}

func TestCheckVirtualKeyBudget(t *testing.T) {
	store, tenantID := seedVirtualKeyTestStore(t)
	ctx := context.Background()

	_, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID: tenantID, UserID: "user-1", APIKeyID: "vk-parent-key",
		Name: "check-budget", Key: "vk-check", SecretHash: repository.HashSecret("vs-check"),
		BudgetUSD: 10.0, BudgetPolicy: repository.BudgetPolicyHardReject,
	})
	if err != nil {
		t.Fatalf("CreateVirtualKey: %v", err)
	}

	result, err := store.CheckVirtualKeyBudget(ctx, "vk-check", 5.0)
	if err != nil {
		t.Fatalf("CheckVirtualKeyBudget: %v", err)
	}
	if !result.Allowed {
		t.Fatal("should allow within budget")
	}
}
