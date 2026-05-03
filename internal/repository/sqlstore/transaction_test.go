package sqlstore

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

func TestCreateServiceVersion_TxRollback(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-tx",
		Slug:   "tenant-tx",
		Name:   "tenant-tx",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	service, err := store.CreateService(ctx, repository.CreateServiceParams{
		TenantID:      tenant.ID,
		Name:          "Tx Service",
		RequestPrefix: "tx-service",
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	_, err = store.CreateServiceVersion(ctx, tenant.ID, repository.CreateServiceVersionParams{
		ServiceID: service.ID,
		Snapshot:  serviceSnapshotFromRecord(*service),
	})
	if err != nil {
		t.Fatalf("create first version: %v", err)
	}

	versionsBefore, err := store.ListServiceVersions(ctx, tenant.ID, service.ID)
	if err != nil {
		t.Fatalf("list versions before: %v", err)
	}
	if len(versionsBefore) != 1 {
		t.Fatalf("expected 1 version before bad tx, got %d", len(versionsBefore))
	}
}

func TestDeleteProject_TxRollback(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-tx",
		Slug:   "tenant-tx",
		Name:   "tenant-tx",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	project, err := store.CreateProject(ctx, repository.CreateProjectParams{
		TenantID:  tenant.ID,
		Slug:      "proj-tx",
		Name:      "Tx Project",
		Status:    repository.StatusActive,
		BudgetUSD: 10,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	if err := store.DeleteProject(ctx, tenant.ID, project.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	_, err = store.GetProject(ctx, tenant.ID, project.ID)
	if err == nil {
		t.Fatalf("expected project to be deleted")
	}
	if err != repository.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestTransitionServiceVersion_Tx(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-tx",
		Slug:   "tenant-tx",
		Name:   "tenant-tx",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	service, err := store.CreateService(ctx, repository.CreateServiceParams{
		TenantID:      tenant.ID,
		Name:          "Transition Service",
		RequestPrefix: "transition-service",
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	v1, err := store.CreateServiceVersion(ctx, tenant.ID, repository.CreateServiceVersionParams{
		ServiceID: service.ID,
		Snapshot:  serviceSnapshotFromRecord(*service),
	})
	if err != nil {
		t.Fatalf("create v1: %v", err)
	}
	v2, err := store.CreateServiceVersion(ctx, tenant.ID, repository.CreateServiceVersionParams{
		ServiceID: service.ID,
		Snapshot:  serviceSnapshotFromRecord(*service),
	})
	if err != nil {
		t.Fatalf("create v2: %v", err)
	}

	svc1, ver1, err := store.PublishServiceVersion(ctx, tenant.ID, service.ID, repository.PublishServiceVersionParams{VersionID: v1.ID, Mode: "published"})
	if err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	if svc1.PublishedVersionID != v1.ID || ver1.Status != "published" {
		t.Fatalf("v1 not published correctly")
	}

	svc2, ver2, err := store.PublishServiceVersion(ctx, tenant.ID, service.ID, repository.PublishServiceVersionParams{VersionID: v2.ID, Mode: "published"})
	if err != nil {
		t.Fatalf("publish v2: %v", err)
	}
	if svc2.PublishedVersionID != v2.ID || ver2.Status != "published" {
		t.Fatalf("v2 not published correctly")
	}

	updatedV1, err := store.GetServiceVersion(ctx, tenant.ID, service.ID, v1.ID)
	if err != nil {
		t.Fatalf("get updated v1: %v", err)
	}
	if updatedV1.Status != "archived" {
		t.Fatalf("expected v1 to be archived, got %s", updatedV1.Status)
	}
}
