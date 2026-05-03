package sqlstore

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

func TestCheckAPIKeyBudget(t *testing.T) {
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
		TenantID:     tenant.ID,
		Key:          "budget-key",
		SecretHash:   repository.HashSecret("secret"),
		Name:         "budget-user",
		Email:        "budget@example.com",
		Role:         repository.RoleTenantUser,
		Quota:        100,
		QPS:          10,
		KeyBudgetUSD: 50,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	identity, err := store.Authenticate(ctx, "budget-key")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	result, err := store.CheckAPIKeyBudget(ctx, identity.APIKeyID, 10)
	if err != nil {
		t.Fatalf("check api key budget: %v", err)
	}
	if !result.Allowed || result.Remaining != 40 {
		t.Fatalf("expected allowed with remaining 40, got allowed=%v remaining=%f", result.Allowed, result.Remaining)
	}
}

func TestCheckProjectBudget(t *testing.T) {
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
	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID:  tenant.ID,
		Slug:      "proj-budget",
		Name:      "Budget Project",
		Status:    repository.StatusActive,
		BudgetUSD: 100,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	result, err := store.CheckProjectBudget(ctx, project.ID, 30)
	if err != nil {
		t.Fatalf("check project budget: %v", err)
	}
	if !result.Allowed || result.Remaining != 70 {
		t.Fatalf("expected allowed with remaining 70, got allowed=%v remaining=%f", result.Allowed, result.Remaining)
	}
}

func TestCheckTenantBudget(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:        "tenant-budget",
		Slug:      "tenant-budget",
		Name:      "Budget Tenant",
		Status:    repository.StatusActive,
		BudgetUSD: 200,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}

	result, err := store.CheckTenantBudget(ctx, tenant.ID, 50)
	if err != nil {
		t.Fatalf("check tenant budget: %v", err)
	}
	if !result.Allowed || result.Remaining != 150 {
		t.Fatalf("expected allowed with remaining 150, got allowed=%v remaining=%f", result.Allowed, result.Remaining)
	}
}

func TestCheckVirtualKeyBudget_BudgetTest(t *testing.T) {
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
	vk, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID:   tenant.ID,
		Name:       "test-vk",
		Key:        "vk-test",
		SecretHash: "hash",
		BudgetUSD:  25,
	})
	if err != nil {
		t.Fatalf("create virtual key: %v", err)
	}

	result, err := store.CheckVirtualKeyBudget(ctx, vk.ID, 5)
	if err != nil {
		t.Fatalf("check virtual key budget: %v", err)
	}
	if !result.Allowed || result.Remaining != 20 {
		t.Fatalf("expected allowed with remaining 20, got allowed=%v remaining=%f", result.Allowed, result.Remaining)
	}
}

func TestConsumeAPIKeyBudget(t *testing.T) {
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
		TenantID:     tenant.ID,
		Key:          "consume-key",
		SecretHash:   repository.HashSecret("secret"),
		Name:         "consume-user",
		Email:        "consume@example.com",
		Role:         repository.RoleTenantUser,
		Quota:        100,
		QPS:          10,
		KeyBudgetUSD: 10,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	identity, err := store.Authenticate(ctx, "consume-key")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	ok, err := store.ConsumeAPIKeyBudget(ctx, identity.APIKeyID, 6)
	if err != nil || !ok {
		t.Fatalf("first consume = (%v,%v), want (true,nil)", ok, err)
	}
	ok, err = store.ConsumeAPIKeyBudget(ctx, identity.APIKeyID, 5)
	if err != nil {
		t.Fatalf("second consume error: %v", err)
	}
	if ok {
		t.Fatalf("expected second consume to fail")
	}
}

func TestConsumeProjectBudget(t *testing.T) {
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
	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID:  tenant.ID,
		Slug:      "proj-consume",
		Name:      "Consume Project",
		Status:    repository.StatusActive,
		BudgetUSD: 20,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	ok, err := store.ConsumeProjectBudget(ctx, project.ID, 15)
	if err != nil || !ok {
		t.Fatalf("first consume = (%v,%v), want (true,nil)", ok, err)
	}
	ok, err = store.ConsumeProjectBudget(ctx, project.ID, 10)
	if err != nil {
		t.Fatalf("second consume error: %v", err)
	}
	if ok {
		t.Fatalf("expected second consume to fail")
	}
}

func TestConsumeTenantBudget(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:        "tenant-consume",
		Slug:      "tenant-consume",
		Name:      "Consume Tenant",
		Status:    repository.StatusActive,
		BudgetUSD: 30,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}

	ok, err := store.ConsumeTenantBudget(ctx, tenant.ID, 20)
	if err != nil || !ok {
		t.Fatalf("first consume = (%v,%v), want (true,nil)", ok, err)
	}
	ok, err = store.ConsumeTenantBudget(ctx, tenant.ID, 15)
	if err != nil {
		t.Fatalf("second consume error: %v", err)
	}
	if ok {
		t.Fatalf("expected second consume to fail")
	}
}

func TestConsumeVirtualKeyBudget_BudgetTest(t *testing.T) {
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
	vk, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID:   tenant.ID,
		Name:       "vk-consume",
		Key:        "vk-consume-key",
		SecretHash: "hash",
		BudgetUSD:  12,
	})
	if err != nil {
		t.Fatalf("create virtual key: %v", err)
	}

	ok, err := store.ConsumeVirtualKeyBudget(ctx, vk.ID, 8)
	if err != nil || !ok {
		t.Fatalf("first consume = (%v,%v), want (true,nil)", ok, err)
	}
	ok, err = store.ConsumeVirtualKeyBudget(ctx, vk.ID, 5)
	if err != nil {
		t.Fatalf("second consume error: %v", err)
	}
	if ok {
		t.Fatalf("expected second consume to fail")
	}
}

func TestConsumeQuota(t *testing.T) {
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
		Key:        "quota-key2",
		SecretHash: repository.HashSecret("secret"),
		Name:       "quota-user",
		Email:      "quota@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      20,
		QPS:        10,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	identity, err := store.Authenticate(ctx, "quota-key2")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	ok, err := store.ConsumeQuota(ctx, identity.UserID, 12)
	if err != nil || !ok {
		t.Fatalf("first consume = (%v,%v), want (true,nil)", ok, err)
	}
	ok, err = store.ConsumeQuota(ctx, identity.UserID, 9)
	if err != nil {
		t.Fatalf("second consume error: %v", err)
	}
	if ok {
		t.Fatalf("expected second consume to fail")
	}
}
