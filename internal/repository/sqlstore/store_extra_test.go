package sqlstore

import (
	"context"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

func TestUserCRUDStatsAndTouchPaths(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-main",
		Slug:   "tenant-main",
		Name:   "Tenant Main",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	created, err := store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:   tenant.ID,
		Name:       "alice",
		Email:      "alice@example.com",
		Role:       repository.RoleTenantAdmin,
		Quota:      100,
		QPS:        5,
		Models:     []string{"gpt-a", "gpt-b"},
		APIKey:     "alice-key",
		SecretHash: repository.HashSecret("alice-secret"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	if created.APIKey != "alice-key" || len(created.Models) != 2 {
		t.Fatalf("CreateUser() = %+v, want api key and models", created)
	}

	users, err := store.ListUsers(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListUsers() error: %v", err)
	}
	if len(users) != 1 || users[0].Name != "alice" {
		t.Fatalf("ListUsers() = %+v, want one alice user", users)
	}

	gotByID, err := store.GetUser(ctx, tenant.ID, created.ID)
	if err != nil {
		t.Fatalf("GetUser(by id) error: %v", err)
	}
	gotByKey, err := store.GetUser(ctx, tenant.ID, "alice-key")
	if err != nil {
		t.Fatalf("GetUser(by api key) error: %v", err)
	}
	if gotByID.ID != gotByKey.ID {
		t.Fatalf("GetUser(by id/key) IDs = (%q,%q), want same ID", gotByID.ID, gotByKey.ID)
	}

	role := repository.RoleTenantUser
	quota := 80
	qps := 9
	models := []string{"claude-a"}
	status := repository.StatusInactive
	updated, err := store.UpdateUser(ctx, tenant.ID, created.ID, repository.UpdateUserParams{
		Role:   &role,
		Quota:  &quota,
		QPS:    &qps,
		Models: &models,
		Status: &status,
	})
	if err != nil {
		t.Fatalf("UpdateUser() error: %v", err)
	}
	if updated.Role != repository.RoleTenantUser || updated.QPS != 9 || len(updated.Models) != 1 || updated.Models[0] != "claude-a" {
		t.Fatalf("UpdateUser() = %+v, want updated fields", updated)
	}

	identity, err := store.Authenticate(ctx, "alice-key")
	if err != nil {
		t.Fatalf("Authenticate(alice-key) error: %v", err)
	}
	if err := store.TouchAPIKey(ctx, identity.APIKeyID, time.Now().UTC()); err != nil {
		t.Fatalf("TouchAPIKey(api key id) error: %v", err)
	}

	if ok, err := store.ConsumeQuota(ctx, identity.UserID, 10); err != nil || !ok {
		t.Fatalf("ConsumeQuota() = (%v,%v), want (true,nil)", ok, err)
	}
	reset, err := store.ResetUserUsage(ctx, tenant.ID, created.ID)
	if err != nil {
		t.Fatalf("ResetUserUsage() error: %v", err)
	}
	if reset.Used != 0 {
		t.Fatalf("ResetUserUsage() used = %d, want %d", reset.Used, 0)
	}

	stats, err := store.Stats(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("Stats() error: %v", err)
	}
	if stats.TotalUsers != 1 || stats.ActiveUsers != 0 {
		t.Fatalf("Stats() = %+v, want total 1 and active 0", stats)
	}

	if err := store.DeleteUser(ctx, tenant.ID, created.ID); err != nil {
		t.Fatalf("DeleteUser() error: %v", err)
	}
	if _, err := store.GetUser(ctx, tenant.ID, created.ID); err != repository.ErrNotFound {
		t.Fatalf("GetUser(after delete) error = %v, want %v", err, repository.ErrNotFound)
	}
}

func TestTenantAndUsageQueryPaths(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantA, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-a",
		Slug:   "tenant-a",
		Name:   "Tenant A",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("EnsureTenant(tenant-a) error: %v", err)
	}
	tenantB, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-b",
		Slug:   "tenant-b",
		Name:   "Tenant B",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("EnsureTenant(tenant-b) error: %v", err)
	}
	if _, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{Slug: "tenant-a"}); err != nil {
		t.Fatalf("EnsureTenant(existing slug) error: %v", err)
	}

	tenants, err := store.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants() error: %v", err)
	}
	if len(tenants) < 2 {
		t.Fatalf("ListTenants() length = %d, want >= %d", len(tenants), 2)
	}

	name := "Tenant A Updated"
	status := repository.StatusInactive
	updatedTenant, err := store.UpdateTenant(ctx, tenantA.ID, repository.UpdateTenantParams{
		Name:   &name,
		Status: &status,
	})
	if err != nil {
		t.Fatalf("UpdateTenant() error: %v", err)
	}
	if updatedTenant.Name != name || updatedTenant.Status != repository.StatusInactive {
		t.Fatalf("UpdateTenant() = %+v, want updated values", updatedTenant)
	}
	gotTenant, err := store.GetTenant(ctx, tenantA.Slug)
	if err != nil {
		t.Fatalf("GetTenant(by slug) error: %v", err)
	}
	if gotTenant.ID != tenantA.ID {
		t.Fatalf("GetTenant(by slug).ID = %q, want %q", gotTenant.ID, tenantA.ID)
	}

	if err := store.ReplaceTenantProviders(ctx, tenantA.ID, []string{"openai-a", "anthropic-a"}); err != nil {
		t.Fatalf("ReplaceTenantProviders() error: %v", err)
	}
	providers, err := store.ListTenantProviders(ctx, tenantA.ID)
	if err != nil {
		t.Fatalf("ListTenantProviders() error: %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("ListTenantProviders() = %v, want 2 providers", providers)
	}

	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenantA.ID,
		Key:        "bootstrap-a",
		SecretHash: repository.HashSecret("secret-a"),
		Name:       "bootstrap user",
		Role:       "",
		Quota:      100,
		QPS:        10,
		Models:     []string{"gpt-a"},
	}); err != nil {
		t.Fatalf("EnsureBootstrapKey(create) error: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenantB.ID,
		Key:        "bootstrap-a",
		SecretHash: repository.HashSecret("secret-b"),
		Name:       "bootstrap user updated",
		Role:       repository.RoleSuperAdmin,
		Quota:      200,
		QPS:        20,
		Models:     []string{"claude-b"},
	}); err != nil {
		t.Fatalf("EnsureBootstrapKey(update) error: %v", err)
	}

	identity, err := store.Authenticate(ctx, "bootstrap-a")
	if err != nil {
		t.Fatalf("Authenticate(bootstrap-a) error: %v", err)
	}
	if identity.TenantID != tenantB.ID || identity.Role != repository.RoleSuperAdmin || len(identity.Models) != 1 || identity.Models[0] != "claude-b" {
		t.Fatalf("Authenticate(bootstrap-a) = %+v, want moved tenant, super admin role, updated models", identity)
	}
	if got, want := defaultRole(""), repository.RoleTenantUser; got != want {
		t.Fatalf("defaultRole(\"\") = %q, want %q", got, want)
	}

	now := time.Now().UTC()
	rows := []struct {
		id        string
		createdAt string
		status    string
		errorType string
		total     int
		latency   int
	}{
		{id: "usage-1", createdAt: now.Add(-24 * time.Hour).Format("2006-01-02 15:04:05"), status: "success", total: 5, latency: 11},
		{id: "usage-2", createdAt: now.Format("2006-01-02 15:04:05"), status: "error", errorType: "upstream", total: 2, latency: 21},
	}
	for _, row := range rows {
		if _, err := store.db.Conn.ExecContext(ctx, store.db.Rebind(`
INSERT INTO usage_records (
	id, tenant_id, user_id, api_key_id, provider_name, model,
	prompt_tokens, completion_tokens, total_tokens, cost, latency_ms,
	status, error_type, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			row.id, tenantB.ID, identity.UserID, identity.APIKeyID, "openai-a", "gpt-a",
			1, row.total-1, row.total, 0.1, row.latency, row.status, row.errorType, row.createdAt,
		); err != nil {
			t.Fatalf("insert usage row %s error: %v", row.id, err)
		}
	}

	detail, err := store.GetUserUsageDetail(ctx, tenantB.ID, identity.UserID, now.Add(-48*time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("GetUserUsageDetail() error: %v", err)
	}
	if len(detail) != 2 {
		t.Fatalf("GetUserUsageDetail() length = %d, want %d", len(detail), 2)
	}

	userTrend, err := store.GetUserUsageTrend(ctx, tenantB.ID, identity.UserID, 7)
	if err != nil {
		t.Fatalf("GetUserUsageTrend() error: %v", err)
	}
	tenantTrend, err := store.GetTenantUsageTrend(ctx, tenantB.ID, 7)
	if err != nil {
		t.Fatalf("GetTenantUsageTrend() error: %v", err)
	}
	if len(userTrend) == 0 || len(tenantTrend) == 0 {
		t.Fatalf("GetUserUsageTrend()/GetTenantUsageTrend() = (%v,%v), want non-empty trends", userTrend, tenantTrend)
	}

	if _, err := store.db.Conn.ExecContext(ctx, `
INSERT INTO users (id, tenant_id, name, email, role, status, quota, used, qps, created_at, updated_at)
VALUES (?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-user", "legacy", "legacy@example.com", repository.RoleTenantUser, repository.StatusActive, 1, 0, 0, now, now,
	); err != nil {
		t.Fatalf("insert legacy user error: %v", err)
	}
	if _, err := store.db.Conn.ExecContext(ctx, `
INSERT INTO usage_records (
	id, tenant_id, user_id, api_key_id, provider_name, model,
	prompt_tokens, completion_tokens, total_tokens, cost, latency_ms,
	status, error_type, created_at
) VALUES (?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"legacy-usage", identity.UserID, identity.APIKeyID, "openai-a", "gpt-a", 1, 1, 2, 0.1, 5, "success", "", now,
	); err != nil {
		t.Fatalf("insert legacy usage error: %v", err)
	}
	if err := store.BackfillDefaultTenant(ctx, tenantB.ID); err != nil {
		t.Fatalf("BackfillDefaultTenant() error: %v", err)
	}

	var userTenantID string
	if err := store.db.Conn.QueryRowContext(ctx, `SELECT tenant_id FROM users WHERE id = ?`, "legacy-user").Scan(&userTenantID); err != nil {
		t.Fatalf("query legacy user tenant_id error: %v", err)
	}
	if userTenantID != tenantB.ID {
		t.Fatalf("legacy user tenant_id = %q, want %q", userTenantID, tenantB.ID)
	}
}
