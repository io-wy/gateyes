package sqlstore

import (
	"context"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

func TestDeleteTenantCascade(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-del",
		Slug: "tenant-del",
		Name: "Tenant Del",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}
	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID: tenant.ID,
		Slug:     "proj-del",
		Name:     "Project Del",
	})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	user, err := store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:   tenant.ID,
		ProjectID:  project.ID,
		Name:       "del-user",
		Email:      "del@example.com",
		Quota:      10,
		QPS:        1,
		APIKey:     "del-key",
		SecretHash: repository.HashSecret("del-secret"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}

	if err := store.CreateUsageRecord(ctx, repository.UsageRecord{
		ID:           "usage-del",
		TenantID:     tenant.ID,
		ProjectID:    project.ID,
		UserID:       user.ID,
		ProviderName: "openai",
		Model:        "gpt-test",
		TotalTokens:  5,
		Cost:         0.1,
		Status:       "success",
		CreatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateUsageRecord() error: %v", err)
	}

	if err := store.DeleteTenant(ctx, tenant.ID); err != nil {
		t.Fatalf("DeleteTenant() error: %v", err)
	}
	if got, err := store.GetTenant(ctx, tenant.ID); err != nil {
		t.Fatalf("GetTenant(after delete) error = %v", err)
	} else if got.Status != repository.StatusInactive {
		t.Fatalf("GetTenant(after delete).Status = %q, want %q", got.Status, repository.StatusInactive)
	}
}

func TestGetProjectUsageSummary(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-pus",
		Slug: "tenant-pus",
		Name: "Tenant PUS",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}
	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID: tenant.ID,
		Slug:     "proj-pus",
		Name:     "Project PUS",
	})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	user, err := store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:   tenant.ID,
		ProjectID:  project.ID,
		Name:       "pus-user",
		Email:      "pus@example.com",
		Quota:      10,
		QPS:        1,
		APIKey:     "pus-key",
		SecretHash: repository.HashSecret("pus-secret"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}

	if err := store.CreateUsageRecord(ctx, repository.UsageRecord{
		ID:           "usage-pus",
		TenantID:     tenant.ID,
		ProjectID:    project.ID,
		UserID:       user.ID,
		ProviderName: "openai",
		Model:        "gpt-test",
		TotalTokens:  10,
		Cost:         0.5,
		Status:       "success",
		CreatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateUsageRecord() error: %v", err)
	}

	summary, err := store.GetProjectUsageSummary(ctx, tenant.ID, project.ID)
	if err != nil {
		t.Fatalf("GetProjectUsageSummary() error: %v", err)
	}
	if summary.TotalRequests != 1 || summary.TotalTokens != 10 || summary.TotalCostUSD != 0.5 {
		t.Fatalf("GetProjectUsageSummary() = %+v, want 1 req/10 tok/0.5 cost", summary)
	}
}

func TestGetBudgetStatus(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:        "tenant-bud",
		Slug:      "tenant-bud",
		Name:      "Tenant Bud",
		BudgetUSD: 100,
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}
	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID:  tenant.ID,
		Slug:      "proj-bud",
		Name:      "Project Bud",
		BudgetUSD: 50,
	})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	_, err = store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:     tenant.ID,
		ProjectID:    project.ID,
		Name:         "bud-user",
		Email:        "bud@example.com",
		Quota:        10,
		QPS:          1,
		KeyBudgetUSD: 20,
		APIKey:       "bud-key",
		SecretHash:   repository.HashSecret("bud-secret"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}

	if ok, err := store.ConsumeTenantBudget(ctx, tenant.ID, 30); err != nil || !ok {
		t.Fatalf("ConsumeTenantBudget() = (%v,%v), want (true,nil)", ok, err)
	}
	if ok, err := store.ConsumeProjectBudget(ctx, project.ID, 20); err != nil || !ok {
		t.Fatalf("ConsumeProjectBudget() = (%v,%v), want (true,nil)", ok, err)
	}

	identity, err := store.Authenticate(ctx, "bud-key")
	if err != nil {
		t.Fatalf("Authenticate() error: %v", err)
	}
	if ok, err := store.ConsumeAPIKeyBudget(ctx, identity.APIKeyID, 10); err != nil || !ok {
		t.Fatalf("ConsumeAPIKeyBudget() = (%v,%v), want (true,nil)", ok, err)
	}

	statuses, err := store.GetBudgetStatus(ctx, tenant.ID, project.ID, identity.APIKeyID)
	if err != nil {
		t.Fatalf("GetBudgetStatus() error: %v", err)
	}
	if len(statuses) != 3 {
		t.Fatalf("GetBudgetStatus() length = %d, want 3", len(statuses))
	}

	var foundTenant, foundProject, foundKey bool
	for _, s := range statuses {
		switch s.Scope {
		case "tenant":
			foundTenant = true
			if s.SpentUSD != 30 {
				t.Fatalf("tenant spent = %v, want 30", s.SpentUSD)
			}
		case "project":
			foundProject = true
			if s.SpentUSD != 20 {
				t.Fatalf("project spent = %v, want 20", s.SpentUSD)
			}
		case "api_key":
			foundKey = true
			if s.SpentUSD != 10 {
				t.Fatalf("api_key spent = %v, want 10", s.SpentUSD)
			}
		}
	}
	if !foundTenant || !foundProject || !foundKey {
		t.Fatalf("GetBudgetStatus() missing scopes")
	}
}

func TestUpdateVirtualKeyAllFields(t *testing.T) {
	store, tenantID := seedVirtualKeyTestStore(t)
	ctx := context.Background()

	record, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID:         tenantID,
		UserID:           "user-1",
		APIKeyID:         "vk-parent-key",
		Name:             "full-upd",
		Key:              "vk-full",
		SecretHash:       repository.HashSecret("vs-full"),
		BudgetUSD:        10,
		BudgetPolicy:     repository.BudgetPolicySoftAlert,
		RateLimitQPS:     5,
		AllowedModels:    []string{"gpt-3"},
		AllowedProviders: []string{"openai"},
		Metadata:         map[string]any{"k": "v"},
		CallbackURL:      "https://old.example.com",
	})
	if err != nil {
		t.Fatalf("CreateVirtualKey: %v", err)
	}

	name := "updated"
	status := repository.StatusInactive
	budget := 99.0
	policy := repository.BudgetPolicyHardReject
	qps := 50
	models := []string{"gpt-4"}
	providers := []string{"anthropic"}
	metadata := map[string]any{"k2": "v2"}
	callback := "https://new.example.com"
	past := time.Now().Add(-1 * time.Hour)
	pastPtr := &past
	updated, err := store.UpdateVirtualKey(ctx, tenantID, record.ID, repository.UpdateVirtualKeyParams{
		Name:             &name,
		Status:           &status,
		BudgetUSD:        &budget,
		BudgetPolicy:     &policy,
		RateLimitQPS:     &qps,
		AllowedModels:    &models,
		AllowedProviders: &providers,
		Metadata:         &metadata,
		CallbackURL:      &callback,
		ExpiresAt:        &pastPtr,
		RevokedAt:        &pastPtr,
	})
	if err != nil {
		t.Fatalf("UpdateVirtualKey: %v", err)
	}
	if updated.Name != name || updated.Status != status || updated.BudgetUSD != budget || updated.BudgetPolicy != policy || updated.RateLimitQPS != qps {
		t.Fatalf("UpdateVirtualKey() basic fields not updated")
	}
	if len(updated.AllowedModels) != 1 || updated.AllowedModels[0] != "gpt-4" {
		t.Fatalf("UpdateVirtualKey() models = %v, want [gpt-4]", updated.AllowedModels)
	}
	if updated.CallbackURL != callback {
		t.Fatalf("UpdateVirtualKey() callback = %q, want %q", updated.CallbackURL, callback)
	}
}

func TestDeleteVirtualKeyByKey(t *testing.T) {
	store, tenantID := seedVirtualKeyTestStore(t)
	ctx := context.Background()

	record, err := store.CreateVirtualKey(ctx, repository.CreateVirtualKeyParams{
		TenantID: tenantID, UserID: "user-1", APIKeyID: "vk-parent-key",
		Name: "del-by-key", Key: "vk-del-key", SecretHash: repository.HashSecret("vs-del-key"),
	})
	if err != nil {
		t.Fatalf("CreateVirtualKey: %v", err)
	}

	if err := store.DeleteVirtualKey(ctx, tenantID, record.Key); err != nil {
		t.Fatalf("DeleteVirtualKey(by key): %v", err)
	}
	if _, err := store.GetVirtualKey(ctx, tenantID, record.ID); err != repository.ErrNotFound {
		t.Fatalf("GetVirtualKey after delete: %v, want ErrNotFound", err)
	}
}

func TestUsageTimeBucketsWeekAndMonth(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-bucket",
		Slug: "tenant-bucket",
		Name: "Tenant Bucket",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}
	user, err := store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:   tenant.ID,
		Name:       "bucket-user",
		Email:      "bucket@example.com",
		Quota:      10,
		QPS:        1,
		APIKey:     "bucket-key",
		SecretHash: repository.HashSecret("bucket-secret"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := store.CreateUsageRecord(ctx, repository.UsageRecord{
			ID:           "usage-bucket-" + string(rune('a'+i)),
			TenantID:     tenant.ID,
			UserID:       user.ID,
			ProviderName: "openai",
			Model:        "gpt-test",
			TotalTokens:  5,
			Cost:         0.1,
			Status:       "success",
			CreatedAt:    now.AddDate(0, 0, -i),
		}); err != nil {
			t.Fatalf("CreateUsageRecord() error: %v", err)
		}
	}

	weekBuckets, err := store.GetUsageTimeBuckets(ctx, repository.UsageFilter{TenantID: tenant.ID}, "week", 10)
	if err != nil {
		t.Fatalf("GetUsageTimeBuckets(week) error: %v", err)
	}
	if len(weekBuckets) == 0 {
		t.Fatalf("GetUsageTimeBuckets(week) empty")
	}

	monthBuckets, err := store.GetUsageTimeBuckets(ctx, repository.UsageFilter{TenantID: tenant.ID}, "month", 10)
	if err != nil {
		t.Fatalf("GetUsageTimeBuckets(month) error: %v", err)
	}
	if len(monthBuckets) == 0 {
		t.Fatalf("GetUsageTimeBuckets(month) empty")
	}
}

func TestGetProjectUsageTrend(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-ptrend",
		Slug: "tenant-ptrend",
		Name: "Tenant PTrend",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}
	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID: tenant.ID,
		Slug:     "proj-ptrend",
		Name:     "Project PTrend",
	})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	user, err := store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:   tenant.ID,
		ProjectID:  project.ID,
		Name:       "ptrend-user",
		Email:      "ptrend@example.com",
		Quota:      10,
		QPS:        1,
		APIKey:     "ptrend-key",
		SecretHash: repository.HashSecret("ptrend-secret"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}

	if err := store.CreateUsageRecord(ctx, repository.UsageRecord{
		ID:           "usage-ptrend",
		TenantID:     tenant.ID,
		ProjectID:    project.ID,
		UserID:       user.ID,
		ProviderName: "openai",
		Model:        "gpt-test",
		TotalTokens:  5,
		Cost:         0.1,
		Status:       "success",
		CreatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateUsageRecord() error: %v", err)
	}

	trend, err := store.GetProjectUsageTrend(ctx, tenant.ID, project.ID, 7)
	if err != nil {
		t.Fatalf("GetProjectUsageTrend() error: %v", err)
	}
	if len(trend) == 0 {
		t.Fatalf("GetProjectUsageTrend() empty")
	}
}
