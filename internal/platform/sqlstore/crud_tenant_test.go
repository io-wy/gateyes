package sqlstore

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

func TestCreateTenant(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-create",
		Slug:   "tenant-create",
		Name:   "Tenant Create",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}
	if tenant.Slug != "tenant-create" || tenant.Name != "Tenant Create" {
		t.Fatalf("EnsureTenant() = %+v, want slug/name", tenant)
	}

	existing, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{Slug: "tenant-create"})
	if err != nil {
		t.Fatalf("EnsureTenant(existing) error: %v", err)
	}
	if existing.ID != tenant.ID {
		t.Fatalf("EnsureTenant(existing) ID = %q, want %q", existing.ID, tenant.ID)
	}
}

func TestGetTenant(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-get",
		Slug: "tenant-get",
		Name: "Tenant Get",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	byID, err := store.GetTenant(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("GetTenant(by id) error: %v", err)
	}
	bySlug, err := store.GetTenant(ctx, tenant.Slug)
	if err != nil {
		t.Fatalf("GetTenant(by slug) error: %v", err)
	}
	if byID.ID != bySlug.ID {
		t.Fatalf("GetTenant() IDs = (%q,%q), want same", byID.ID, bySlug.ID)
	}

	if _, err := store.GetTenant(ctx, "nonexistent"); err != repository.ErrNotFound {
		t.Fatalf("GetTenant(nonexistent) error = %v, want %v", err, repository.ErrNotFound)
	}
}

func TestListTenants(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"tenant-a", "tenant-b"} {
		if _, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
			ID:   id,
			Slug: id,
			Name: id,
		}); err != nil {
			t.Fatalf("EnsureTenant(%s) error: %v", id, err)
		}
	}

	tenants, err := store.ListTenants(ctx)
	if err != nil {
		t.Fatalf("ListTenants() error: %v", err)
	}
	if len(tenants) != 2 {
		t.Fatalf("ListTenants() length = %d, want 2", len(tenants))
	}
}

func TestUpdateTenant(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-upd",
		Slug: "tenant-upd",
		Name: "Tenant Upd",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	name := "Updated Name"
	status := repository.StatusInactive
	budget := 100.0
	updated, err := store.UpdateTenant(ctx, tenant.ID, repository.UpdateTenantParams{
		Name:      &name,
		Status:    &status,
		BudgetUSD: &budget,
	})
	if err != nil {
		t.Fatalf("UpdateTenant() error: %v", err)
	}
	if updated.Name != name || updated.Status != status || updated.BudgetUSD != budget {
		t.Fatalf("UpdateTenant() = %+v, want updated fields", updated)
	}
}

func TestReplaceTenantProviders(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-provs",
		Slug: "tenant-provs",
		Name: "Tenant Provs",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	if err := store.ReplaceTenantProviders(ctx, tenant.ID, []string{"p1", "p2", "p3"}); err != nil {
		t.Fatalf("ReplaceTenantProviders() error: %v", err)
	}
	providers, err := store.ListTenantProviders(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListTenantProviders() error: %v", err)
	}
	if len(providers) != 3 {
		t.Fatalf("ListTenantProviders() = %v, want 3", providers)
	}

	if err := store.ReplaceTenantProviders(ctx, tenant.ID, []string{"p1"}); err != nil {
		t.Fatalf("ReplaceTenantProviders(replace) error: %v", err)
	}
	providers, err = store.ListTenantProviders(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListTenantProviders() error: %v", err)
	}
	if len(providers) != 1 || providers[0] != "p1" {
		t.Fatalf("ListTenantProviders() = %v, want [p1]", providers)
	}
}
