package sqlstore

import (
	"context"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

func TestCreateResponse(t *testing.T) {
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
		SecretHash: repository.HashSecret("secret"),
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

	record := repository.ResponseRecord{
		ID:           "resp-create",
		TenantID:     tenant.ID,
		ProjectID:    identity.ProjectID,
		UserID:       identity.UserID,
		APIKeyID:     identity.APIKeyID,
		ProviderName: "openai-primary",
		Model:        "gpt-test",
		Status:       "in_progress",
		RequestBody:  []byte(`{"input":"hello"}`),
	}
	if err := store.CreateResponse(ctx, record); err != nil {
		t.Fatalf("create response: %v", err)
	}

	got, err := store.GetResponse(ctx, tenant.ID, "resp-create")
	if err != nil {
		t.Fatalf("get response: %v", err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("expected status in_progress, got %s", got.Status)
	}
}

func TestCreateUsageRecord(t *testing.T) {
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
		Key:        "usage-key",
		SecretHash: repository.HashSecret("secret"),
		Name:       "usage-user",
		Email:      "usage@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      100,
		QPS:        10,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	identity, err := store.Authenticate(ctx, "usage-key")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	record := repository.UsageRecord{
		ID:               "usage-create",
		TenantID:         tenant.ID,
		ProjectID:        identity.ProjectID,
		UserID:           identity.UserID,
		APIKeyID:         identity.APIKeyID,
		ProviderName:     "openai-primary",
		Model:            "gpt-test",
		PromptTokens:     3,
		CompletionTokens: 2,
		TotalTokens:      5,
		Cost:             1.5,
		LatencyMs:        42,
		Status:           "success",
		CreatedAt:        time.Now().UTC(),
	}
	if err := store.CreateUsageRecord(ctx, record); err != nil {
		t.Fatalf("create usage record: %v", err)
	}

	summary, err := store.GetUsageSummary(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("usage summary: %v", err)
	}
	if summary.SuccessRequests != 1 || summary.TotalTokens != 5 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestGetResponse(t *testing.T) {
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
		Key:        "get-key",
		SecretHash: repository.HashSecret("secret"),
		Name:       "get-user",
		Email:      "get@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      100,
		QPS:        10,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	identity, err := store.Authenticate(ctx, "get-key")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	if err := store.CreateResponse(ctx, repository.ResponseRecord{
		ID:           "resp-get",
		TenantID:     tenant.ID,
		ProjectID:    identity.ProjectID,
		UserID:       identity.UserID,
		APIKeyID:     identity.APIKeyID,
		ProviderName: "openai-primary",
		Model:        "gpt-test",
		Status:       "completed",
		RequestBody:  []byte(`{"input":"hello"}`),
		ResponseBody: []byte(`{"output":"world"}`),
	}); err != nil {
		t.Fatalf("create response: %v", err)
	}

	got, err := store.GetResponse(ctx, tenant.ID, "resp-get")
	if err != nil {
		t.Fatalf("get response: %v", err)
	}
	if got.ID != "resp-get" || got.Status != "completed" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestListResponses(t *testing.T) {
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
		Key:        "list-key",
		SecretHash: repository.HashSecret("secret"),
		Name:       "list-user",
		Email:      "list@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      100,
		QPS:        10,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	identity, err := store.Authenticate(ctx, "list-key")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := store.CreateResponse(ctx, repository.ResponseRecord{
			ID:           "resp-list-" + string(rune('a'+i)),
			TenantID:     tenant.ID,
			ProjectID:    identity.ProjectID,
			UserID:       identity.UserID,
			APIKeyID:     identity.APIKeyID,
			ProviderName: "openai-primary",
			Model:        "gpt-test",
			Status:       "completed",
			RequestBody:  []byte(`{}`),
		}); err != nil {
			t.Fatalf("create response %d: %v", i, err)
		}
	}

	items, err := store.ListResponses(ctx, tenant.ID, repository.ResponseFilter{Limit: 2})
	if err != nil {
		t.Fatalf("list responses: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(items))
	}

	count, err := store.CountResponses(ctx, tenant.ID, repository.ResponseFilter{})
	if err != nil {
		t.Fatalf("count responses: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected count 3, got %d", count)
	}
}

func TestUpdateResponse(t *testing.T) {
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
		Key:        "upd-key",
		SecretHash: repository.HashSecret("secret"),
		Name:       "upd-user",
		Email:      "upd@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      100,
		QPS:        10,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}
	identity, err := store.Authenticate(ctx, "upd-key")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	if err := store.CreateResponse(ctx, repository.ResponseRecord{
		ID:           "resp-upd",
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
		ID:           "resp-upd",
		TenantID:     tenant.ID,
		ProviderName: "openai-primary",
		Model:        "gpt-test",
		Status:       "completed",
		ResponseBody: []byte(`{"output":"done"}`),
	}); err != nil {
		t.Fatalf("update response: %v", err)
	}

	got, err := store.GetResponse(ctx, tenant.ID, "resp-upd")
	if err != nil {
		t.Fatalf("get response: %v", err)
	}
	if got.Status != "completed" || string(got.ResponseBody) != `{"output":"done"}` {
		t.Fatalf("unexpected updated response: status=%s body=%s", got.Status, string(got.ResponseBody))
	}
}
