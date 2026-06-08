package sqlstore

import (
	"context"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

func seedTenantAndUser(t *testing.T, store *Store) (string, string) {
	t.Helper()
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-ak",
		Slug: "tenant-ak",
		Name: "Tenant AK",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	user, err := store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:   tenant.ID,
		Name:       "ak-user",
		Email:      "ak@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      100,
		QPS:        5,
		APIKey:     "user-key",
		SecretHash: repository.HashSecret("user-secret"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	return tenant.ID, user.ID
}

func TestCreateAPIKey(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, userID := seedTenantAndUser(t, store)
	key, err := store.CreateAPIKey(ctx, repository.CreateAPIKeyParams{
		UserID:           userID,
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
	if key.TenantID != tenantID || key.RateLimitQPS != 7 || len(key.AllowedProviders) != 1 {
		t.Fatalf("CreateAPIKey() = %+v, want tenant/qps/providers", key)
	}
}

func TestGetAPIKey(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, userID := seedTenantAndUser(t, store)
	created, err := store.CreateAPIKey(ctx, repository.CreateAPIKeyParams{
		UserID:     userID,
		Key:        "get-key",
		SecretHash: repository.HashSecret("get-secret"),
	})
	if err != nil {
		t.Fatalf("CreateAPIKey() error: %v", err)
	}

	byID, err := store.GetAPIKey(ctx, tenantID, created.ID)
	if err != nil {
		t.Fatalf("GetAPIKey(by id) error: %v", err)
	}
	byKey, err := store.GetAPIKey(ctx, tenantID, created.Key)
	if err != nil {
		t.Fatalf("GetAPIKey(by key) error: %v", err)
	}
	if byID.ID != byKey.ID {
		t.Fatalf("GetAPIKey() IDs = (%q,%q), want same", byID.ID, byKey.ID)
	}

	if _, err := store.GetAPIKey(ctx, tenantID, "nonexistent"); err != repository.ErrNotFound {
		t.Fatalf("GetAPIKey(nonexistent) error = %v, want %v", err, repository.ErrNotFound)
	}
}

func TestListAPIKeys(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, userID := seedTenantAndUser(t, store)
	for _, key := range []string{"key-a", "key-b"} {
		if _, err := store.CreateAPIKey(ctx, repository.CreateAPIKeyParams{
			UserID:     userID,
			Key:        key,
			SecretHash: repository.HashSecret(key + "-secret"),
		}); err != nil {
			t.Fatalf("CreateAPIKey(%s) error: %v", key, err)
		}
	}

	keys, err := store.ListAPIKeys(ctx, tenantID, repository.APIKeyFilter{UserID: userID})
	if err != nil {
		t.Fatalf("ListAPIKeys() error: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("ListAPIKeys() length = %d, want 3 (1 user key + 2 scoped)", len(keys))
	}
}

func TestUpdateAPIKey(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, userID := seedTenantAndUser(t, store)
	key, err := store.CreateAPIKey(ctx, repository.CreateAPIKeyParams{
		UserID:       userID,
		Key:          "upd-key",
		SecretHash:   repository.HashSecret("upd-secret"),
		BudgetUSD:    5,
		RateLimitQPS: 1,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey() error: %v", err)
	}

	status := repository.StatusInactive
	budget := 20.0
	qps := 15
	updated, err := store.UpdateAPIKey(ctx, tenantID, key.ID, repository.UpdateAPIKeyParams{
		Status:       &status,
		BudgetUSD:    &budget,
		RateLimitQPS: &qps,
	})
	if err != nil {
		t.Fatalf("UpdateAPIKey() error: %v", err)
	}
	if updated.Status != status || updated.BudgetUSD != budget || updated.RateLimitQPS != qps {
		t.Fatalf("UpdateAPIKey() = %+v, want updated fields", updated)
	}
}

func TestRotateAPIKey(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, userID := seedTenantAndUser(t, store)
	key, err := store.CreateAPIKey(ctx, repository.CreateAPIKeyParams{
		UserID:     userID,
		Key:        "rot-key",
		SecretHash: repository.HashSecret("rot-secret"),
		Status:     repository.StatusInactive,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey() error: %v", err)
	}

	rotated, err := store.RotateAPIKey(ctx, tenantID, key.ID, repository.RotateAPIKeyParams{
		NewKey:        "rot-key-new",
		NewSecretHash: repository.HashSecret("rot-secret-new"),
	})
	if err != nil {
		t.Fatalf("RotateAPIKey() error: %v", err)
	}
	if rotated.Key != "rot-key-new" || rotated.Status != repository.StatusActive {
		t.Fatalf("RotateAPIKey() = %+v, want new key active", rotated)
	}
}

func TestTouchAPIKey(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, userID := seedTenantAndUser(t, store)
	key, err := store.CreateAPIKey(ctx, repository.CreateAPIKeyParams{
		UserID:     userID,
		Key:        "touch-key",
		SecretHash: repository.HashSecret("touch-secret"),
	})
	if err != nil {
		t.Fatalf("CreateAPIKey() error: %v", err)
	}

	now := time.Now().UTC()
	if err := store.TouchAPIKey(ctx, key.ID, now); err != nil {
		t.Fatalf("TouchAPIKey() error: %v", err)
	}

	got, err := store.GetAPIKey(ctx, "", key.ID)
	if err != nil {
		t.Fatalf("GetAPIKey() error: %v", err)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(now) {
		t.Fatalf("TouchAPIKey() last_used_at not set correctly")
	}
}

func TestDeleteAPIKey(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, userID := seedTenantAndUser(t, store)
	key, err := store.CreateAPIKey(ctx, repository.CreateAPIKeyParams{
		UserID:     userID,
		Key:        "del-key",
		SecretHash: repository.HashSecret("del-secret"),
	})
	if err != nil {
		t.Fatalf("CreateAPIKey() error: %v", err)
	}

	if err := store.DeleteUser(ctx, tenantID, userID); err != nil {
		t.Fatalf("DeleteUser() error: %v", err)
	}
	got, err := store.GetAPIKey(ctx, tenantID, key.ID)
	if err != nil {
		t.Fatalf("GetAPIKey error = %v", err)
	}
	if got.Status != repository.StatusRevoked {
		t.Fatalf("expected Status=revoked, got %q", got.Status)
	}
}
