package sqlstore

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

func TestCreateProject(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-proj",
		Slug: "tenant-proj",
		Name: "Tenant Proj",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID:  tenant.ID,
		Slug:      "proj-a",
		Name:      "Project A",
		Status:    repository.StatusActive,
		BudgetUSD: 50,
	})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if project.TenantID != tenant.ID || project.Slug != "proj-a" || project.BudgetUSD != 50 {
		t.Fatalf("CreateProject() = %+v, want tenant/slug/budget", project)
	}
}

func TestGetProject(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-proj-get",
		Slug: "tenant-proj-get",
		Name: "Tenant Proj Get",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID: tenant.ID,
		Slug:     "proj-get",
		Name:     "Project Get",
	})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	byID, err := store.GetProject(ctx, tenant.ID, project.ID)
	if err != nil {
		t.Fatalf("GetProject(by id) error: %v", err)
	}
	bySlug, err := store.GetProject(ctx, tenant.ID, project.Slug)
	if err != nil {
		t.Fatalf("GetProject(by slug) error: %v", err)
	}
	if byID.ID != bySlug.ID {
		t.Fatalf("GetProject() IDs = (%q,%q), want same", byID.ID, bySlug.ID)
	}

	if _, err := store.GetProject(ctx, tenant.ID, "nonexistent"); err != repository.ErrNotFound {
		t.Fatalf("GetProject(nonexistent) error = %v, want %v", err, repository.ErrNotFound)
	}
}

func TestListProjects(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-proj-list",
		Slug: "tenant-proj-list",
		Name: "Tenant Proj List",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	for _, slug := range []string{"proj-1", "proj-2"} {
		if _, err := store.CreateProject(ctx, repository.CreateProjectParams{
			TenantID: tenant.ID,
			Slug:     slug,
			Name:     slug,
		}); err != nil {
			t.Fatalf("CreateProject(%s) error: %v", slug, err)
		}
	}

	projects, err := store.ListProjects(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("ListProjects() error: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("ListProjects() length = %d, want 2", len(projects))
	}
}

func TestUpdateProject(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-proj-upd",
		Slug: "tenant-proj-upd",
		Name: "Tenant Proj Upd",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID: tenant.ID,
		Slug:     "proj-upd",
		Name:     "Project Upd",
	})
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	name := "Updated Name"
	status := repository.StatusInactive
	budget := 99.0
	updated, err := store.UpdateProject(ctx, tenant.ID, project.ID, repository.UpdateProjectParams{
		Name:      &name,
		Status:    &status,
		BudgetUSD: &budget,
	})
	if err != nil {
		t.Fatalf("UpdateProject() error: %v", err)
	}
	if updated.Name != name || updated.Status != status || updated.BudgetUSD != budget {
		t.Fatalf("UpdateProject() = %+v, want updated fields", updated)
	}
}
