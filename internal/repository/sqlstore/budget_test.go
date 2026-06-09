package sqlstore

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/testutil"
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

func TestBudgetCacheRedis(t *testing.T) {
	rdb := testutil.NewRedisClient(t)
	defer rdb.Close()

	store := newTestStore(t)
	store.SetRedis(rdb)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:        "tenant-cache",
		Slug:      "tenant-cache",
		Name:      "Cache Tenant",
		Status:    repository.StatusActive,
		BudgetUSD: 100,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}

	// 1st check hits DB and writes cache
	r1, err := store.CheckTenantBudget(ctx, tenant.ID, 10)
	if err != nil {
		t.Fatalf("first check: %v", err)
	}
	if !r1.Allowed || r1.Remaining != 90 {
		t.Fatalf("first check: want allowed=90, got allowed=%v remaining=%f", r1.Allowed, r1.Remaining)
	}

	// Verify cache exists
	cacheKey := "budget:tenant:" + tenant.ID
	_, err = rdb.Get(ctx, cacheKey).Result()
	if err != nil {
		t.Fatalf("expected cache key %q to exist after first check: %v", cacheKey, err)
	}

	// Modify DB directly (bypass cache) to simulate another writer
	if _, err := store.db.Conn.ExecContext(ctx, store.db.Rebind(
		"UPDATE tenants SET spent_usd = 50 WHERE id = ?"), tenant.ID); err != nil {
		t.Fatalf("direct update: %v", err)
	}

	// 2nd check hits cache (stale value)
	r2, err := store.CheckTenantBudget(ctx, tenant.ID, 10)
	if err != nil {
		t.Fatalf("second check: %v", err)
	}
	if r2.Remaining != 90 {
		t.Fatalf("second check should read stale cache: want 90, got %f", r2.Remaining)
	}

	// Consume invalidates cache
	ok, err := store.ConsumeTenantBudget(ctx, tenant.ID, 5)
	if err != nil || !ok {
		t.Fatalf("consume: (%v,%v)", ok, err)
	}

	// 3rd check hits DB (cache invalidated)
	r3, err := store.CheckTenantBudget(ctx, tenant.ID, 10)
	if err != nil {
		t.Fatalf("third check: %v", err)
	}
	// DB has spent=55 (50 direct + 5 consume), budget=100, estimated=10 => remaining=35
	if r3.Remaining != 35 {
		t.Fatalf("third check after invalidate: want 35, got %f", r3.Remaining)
	}
}
