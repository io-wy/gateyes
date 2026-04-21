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
	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID:  tenant.ID,
		Slug:      "app-a",
		Name:      "App A",
		Status:    repository.StatusActive,
		BudgetUSD: 100,
	})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	created, err := store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:     tenant.ID,
		ProjectID:    project.ID,
		Name:         "alice",
		Email:        "alice@example.com",
		Role:         repository.RoleTenantAdmin,
		Quota:        100,
		QPS:          5,
		KeyBudgetUSD: 9.5,
		Models:       []string{"gpt-a", "gpt-b"},
		APIKey:       "alice-key",
		SecretHash:   repository.HashSecret("alice-secret"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	if created.APIKey != "alice-key" || len(created.Models) != 2 || created.ProjectID != project.ID || created.KeyBudgetUSD != 9.5 {
		t.Fatalf("CreateUser() = %+v, want api key/models/project/key budget", created)
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
	projectID := ""
	keyBudget := 20.0
	updated, err := store.UpdateUser(ctx, tenant.ID, created.ID, repository.UpdateUserParams{
		Role:         &role,
		Quota:        &quota,
		QPS:          &qps,
		ProjectID:    &projectID,
		KeyBudgetUSD: &keyBudget,
		Models:       &models,
		Status:       &status,
	})
	if err != nil {
		t.Fatalf("UpdateUser() error: %v", err)
	}
	if updated.Role != repository.RoleTenantUser || updated.QPS != 9 || len(updated.Models) != 1 || updated.Models[0] != "claude-a" || updated.ProjectID != "" || updated.KeyBudgetUSD != 20 {
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

func TestAPIKeyLifecycleUsageBreakdownAndResponseTrace(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:        "tenant-budget",
		Slug:      "tenant-budget",
		Name:      "Tenant Budget",
		Status:    repository.StatusActive,
		BudgetUSD: 20,
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}
	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID:  tenant.ID,
		Slug:      "proj-budget",
		Name:      "Project Budget",
		Status:    repository.StatusActive,
		BudgetUSD: 10,
	})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	user, err := store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:     tenant.ID,
		ProjectID:    project.ID,
		Name:         "alice",
		Email:        "alice@example.com",
		Role:         repository.RoleTenantAdmin,
		Quota:        100,
		QPS:          5,
		KeyBudgetUSD: 5,
		APIKey:       "bootstrap-key",
		SecretHash:   repository.HashSecret("bootstrap-secret"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}

	keyRecord, err := store.CreateAPIKey(ctx, repository.CreateAPIKeyParams{
		UserID:           user.ID,
		ProjectID:        project.ID,
		Key:              "scoped-key",
		SecretHash:       repository.HashSecret("scoped-secret"),
		BudgetUSD:        8,
		RateLimitQPS:     7,
		AllowedModels:    []string{"gpt-4o-mini"},
		AllowedProviders: []string{"openai-primary"},
	})
	if err != nil {
		t.Fatalf("CreateAPIKey() error: %v", err)
	}
	if keyRecord.RateLimitQPS != 7 || len(keyRecord.AllowedProviders) != 1 || keyRecord.AllowedProviders[0] != "openai-primary" {
		t.Fatalf("CreateAPIKey() = %+v, want scoped key fields", keyRecord)
	}

	keys, err := store.ListAPIKeys(ctx, tenant.ID, repository.APIKeyFilter{UserID: user.ID})
	if err != nil {
		t.Fatalf("ListAPIKeys() error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("ListAPIKeys() length = %d, want %d", len(keys), 2)
	}

	inactive := repository.StatusInactive
	newBudget := 9.0
	newQPS := 11
	updatedKey, err := store.UpdateAPIKey(ctx, tenant.ID, keyRecord.ID, repository.UpdateAPIKeyParams{
		Status:       &inactive,
		BudgetUSD:    &newBudget,
		RateLimitQPS: &newQPS,
	})
	if err != nil {
		t.Fatalf("UpdateAPIKey() error: %v", err)
	}
	if updatedKey.Status != repository.StatusInactive || updatedKey.BudgetUSD != newBudget || updatedKey.RateLimitQPS != newQPS {
		t.Fatalf("UpdateAPIKey() = %+v, want updated status/budget/qps", updatedKey)
	}

	rotatedKey, err := store.RotateAPIKey(ctx, tenant.ID, keyRecord.ID, repository.RotateAPIKeyParams{
		NewKey:        "scoped-key-rotated",
		NewSecretHash: repository.HashSecret("rotated-secret"),
	})
	if err != nil {
		t.Fatalf("RotateAPIKey() error: %v", err)
	}
	if rotatedKey.Key != "scoped-key-rotated" || rotatedKey.Status != repository.StatusActive {
		t.Fatalf("RotateAPIKey() = %+v, want rotated active key", rotatedKey)
	}

	revoked := repository.StatusRevoked
	now := time.Now().UTC()
	revokedKey, err := store.UpdateAPIKey(ctx, tenant.ID, rotatedKey.ID, repository.UpdateAPIKeyParams{
		Status:    &revoked,
		RevokedAt: &now,
	})
	if err != nil {
		t.Fatalf("Revoke API key error: %v", err)
	}
	if revokedKey.Status != repository.StatusRevoked || revokedKey.RevokedAt == nil {
		t.Fatalf("revoked API key = %+v, want revoked status and revoked_at", revokedKey)
	}

	if ok, err := store.ConsumeTenantBudget(ctx, tenant.ID, 3.5); err != nil || !ok {
		t.Fatalf("ConsumeTenantBudget() = (%v,%v), want (true,nil)", ok, err)
	}
	tenantAfterBudget, err := store.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("GetTenant() error: %v", err)
	}
	if tenantAfterBudget.SpentUSD != 3.5 {
		t.Fatalf("tenant spent_usd = %v, want %v", tenantAfterBudget.SpentUSD, 3.5)
	}

	usageRows := []repository.UsageRecord{
		{
			ID:               "usage-breakdown-1",
			TenantID:         tenant.ID,
			ProjectID:        project.ID,
			UserID:           user.ID,
			APIKeyID:         rotatedKey.ID,
			ProviderName:     "openai-primary",
			Model:            "gpt-4o-mini",
			PromptTokens:     3,
			CompletionTokens: 2,
			TotalTokens:      5,
			Cost:             0.3,
			LatencyMs:        20,
			Status:           "success",
			CreatedAt:        now.Add(-2 * time.Hour),
		},
		{
			ID:               "usage-breakdown-2",
			TenantID:         tenant.ID,
			ProjectID:        project.ID,
			UserID:           user.ID,
			APIKeyID:         rotatedKey.ID,
			ProviderName:     "openai-primary",
			Model:            "gpt-4o-mini",
			PromptTokens:     2,
			CompletionTokens: 1,
			TotalTokens:      3,
			Cost:             0.2,
			LatencyMs:        30,
			Status:           "error",
			ErrorType:        "upstream_error",
			CreatedAt:        now.Add(-time.Hour),
		},
	}
	for _, row := range usageRows {
		if err := store.CreateUsageRecord(ctx, row); err != nil {
			t.Fatalf("CreateUsageRecord(%s) error: %v", row.ID, err)
		}
	}

	summary, err := store.GetUsageSummaryFiltered(ctx, repository.UsageFilter{
		TenantID:  tenant.ID,
		APIKeyID:  rotatedKey.ID,
		StartTime: now.Add(-24 * time.Hour),
		EndTime:   now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("GetUsageSummaryFiltered() error: %v", err)
	}
	if summary.TotalRequests != 2 || summary.TotalCostUSD != 0.5 {
		t.Fatalf("GetUsageSummaryFiltered() = %+v, want 2 requests and 0.5 cost", summary)
	}

	breakdown, err := store.GetUsageBreakdown(ctx, repository.UsageFilter{
		TenantID: tenant.ID,
		APIKeyID: rotatedKey.ID,
	}, "provider")
	if err != nil {
		t.Fatalf("GetUsageBreakdown() error: %v", err)
	}
	if len(breakdown) != 1 || breakdown[0].Dimension != "openai-primary" || breakdown[0].TotalCostUSD != 0.5 {
		t.Fatalf("GetUsageBreakdown() = %+v, want provider cost aggregation", breakdown)
	}

	timeBuckets, err := store.GetUsageTimeBuckets(ctx, repository.UsageFilter{
		TenantID: tenant.ID,
		APIKeyID: rotatedKey.ID,
	}, "day", 10)
	if err != nil {
		t.Fatalf("GetUsageTimeBuckets() error: %v", err)
	}
	if len(timeBuckets) == 0 || timeBuckets[len(timeBuckets)-1].TotalRequests != 2 {
		t.Fatalf("GetUsageTimeBuckets() = %+v, want aggregated day bucket", timeBuckets)
	}

	traceBody := []byte(`{"response_id":"resp-trace","status":"success","ordered_candidates":["openai-primary"]}`)
	if err := store.CreateResponse(ctx, repository.ResponseRecord{
		ID:             "resp-trace",
		TenantID:       tenant.ID,
		ProjectID:      project.ID,
		UserID:         user.ID,
		APIKeyID:       rotatedKey.ID,
		ProviderName:   "openai-primary",
		Model:          "gpt-4o-mini",
		Status:         "completed",
		ResponseBody:   []byte(`{"id":"resp-trace","object":"response"}`),
		RouteTraceBody: traceBody,
	}); err != nil {
		t.Fatalf("CreateResponse() error: %v", err)
	}
	record, err := store.GetResponse(ctx, tenant.ID, "resp-trace")
	if err != nil {
		t.Fatalf("GetResponse() error: %v", err)
	}
	if string(record.RouteTraceBody) != string(traceBody) {
		t.Fatalf("GetResponse().RouteTraceBody = %q, want %q", string(record.RouteTraceBody), string(traceBody))
	}
}

func TestAuditLogLifecycle(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-audit",
		Slug:   "tenant-audit",
		Name:   "Tenant Audit",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	if err := store.CreateAuditLog(ctx, repository.AuditLogRecord{
		ID:            "audit-1",
		TenantID:      tenant.ID,
		ActorUserID:   "user-1",
		ActorAPIKeyID: "key-1",
		ActorRole:     repository.RoleTenantAdmin,
		Action:        "project.create",
		ResourceType:  "project",
		ResourceID:    "project-1",
		RequestID:     "req-1",
		IPAddress:     "127.0.0.1",
		Payload:       []byte(`{"name":"Project A"}`),
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateAuditLog() error: %v", err)
	}

	items, err := store.ListAuditLogs(ctx, tenant.ID, repository.AuditLogFilter{
		Action:       "project.create",
		ResourceType: "project",
		ResourceID:   "project-1",
		ActorUserID:  "user-1",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("ListAuditLogs() error: %v", err)
	}
	if len(items) != 1 || items[0].Action != "project.create" || string(items[0].Payload) != `{"name":"Project A"}` {
		t.Fatalf("ListAuditLogs() = %+v, want one matching audit record", items)
	}
}
