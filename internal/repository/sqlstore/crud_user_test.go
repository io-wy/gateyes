package sqlstore

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

func TestCreateUser(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-user",
		Slug: "tenant-user",
		Name: "Tenant User",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	user, err := store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:   tenant.ID,
		Name:       "alice",
		Email:      "alice@example.com",
		Role:       repository.RoleTenantAdmin,
		Quota:      100,
		QPS:        5,
		KeyBudgetUSD: 10,
		Models:     []string{"gpt-a"},
		APIKey:     "alice-key",
		SecretHash: repository.HashSecret("alice-secret"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}
	if user.Name != "alice" || user.Role != repository.RoleTenantAdmin || len(user.Models) != 1 {
		t.Fatalf("CreateUser() = %+v, want name/role/models", user)
	}
}

func TestGetUser(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-user-get",
		Slug: "tenant-user-get",
		Name: "Tenant User Get",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	created, err := store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:   tenant.ID,
		Name:       "bob",
		Email:      "bob@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      50,
		QPS:        2,
		APIKey:     "bob-key",
		SecretHash: repository.HashSecret("bob-secret"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}

	byID, err := store.GetUser(ctx, tenant.ID, created.ID)
	if err != nil {
		t.Fatalf("GetUser(by id) error: %v", err)
	}
	byKey, err := store.GetUser(ctx, tenant.ID, "bob-key")
	if err != nil {
		t.Fatalf("GetUser(by key) error: %v", err)
	}
	if byID.ID != byKey.ID {
		t.Fatalf("GetUser() IDs = (%q,%q), want same", byID.ID, byKey.ID)
	}

	if _, err := store.GetUser(ctx, tenant.ID, "nonexistent"); err != repository.ErrNotFound {
		t.Fatalf("GetUser(nonexistent) error = %v, want %v", err, repository.ErrNotFound)
	}
}

func TestListUsers(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-user-list",
		Slug: "tenant-user-list",
		Name: "Tenant User List",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	for _, name := range []string{"alice", "bob"} {
		if _, err := store.CreateUser(ctx, repository.CreateUserParams{
			TenantID:   tenant.ID,
			Name:       name,
			Email:      name + "@example.com",
			Role:       repository.RoleTenantUser,
			Quota:      10,
			QPS:        1,
			APIKey:     name + "-key",
			SecretHash: repository.HashSecret(name + "-secret"),
		}); err != nil {
			t.Fatalf("CreateUser(%s) error: %v", name, err)
		}
	}

	users, err := store.ListUsers(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListUsers() error: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("ListUsers() length = %d, want 2", len(users))
	}
}

func TestUpdateUser(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-user-upd",
		Slug: "tenant-user-upd",
		Name: "Tenant User Upd",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	user, err := store.CreateUser(ctx, repository.CreateUserParams{
		TenantID:   tenant.ID,
		Name:       "charlie",
		Email:      "charlie@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      50,
		QPS:        2,
		APIKey:     "charlie-key",
		SecretHash: repository.HashSecret("charlie-secret"),
	})
	if err != nil {
		t.Fatalf("CreateUser() error: %v", err)
	}

	role := repository.RoleTenantAdmin
	quota := 200
	qps := 10
	models := []string{"claude-x"}
	status := repository.StatusInactive
	updated, err := store.UpdateUser(ctx, tenant.ID, user.ID, repository.UpdateUserParams{
		Role:   &role,
		Quota:  &quota,
		QPS:    &qps,
		Models: &models,
		Status: &status,
	})
	if err != nil {
		t.Fatalf("UpdateUser() error: %v", err)
	}
	if updated.Role != role || updated.Quota != quota || updated.QPS != qps || updated.Status != status {
		t.Fatalf("UpdateUser() = %+v, want updated fields", updated)
	}
	if len(updated.Models) != 1 || updated.Models[0] != "claude-x" {
		t.Fatalf("UpdateUser() models = %v, want [claude-x]", updated.Models)
	}
}
