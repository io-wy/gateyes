package sqlstore

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

func seedTenantAndService(t *testing.T, store *Store) (string, *repository.ServiceRecord) {
	t.Helper()
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-svc",
		Slug: "tenant-svc",
		Name: "Tenant Svc",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	service, err := store.CreateService(ctx, repository.CreateServiceParams{
		TenantID:        tenant.ID,
		Name:            "My Service",
		RequestPrefix:   "my-svc",
		Description:     "desc",
		DefaultProvider: "openai-primary",
		DefaultModel:    "gpt-test",
		Enabled:         true,
		Config: repository.ServiceConfig{
			Surfaces: []string{"responses"},
			Metadata: map[string]any{"k": "v"},
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error: %v", err)
	}
	return tenant.ID, service
}

func TestCreateService(t *testing.T) {
	store := newTestStore(t)

	tenantID, service := seedTenantAndService(t, store)
	if service.TenantID != tenantID || service.Name != "My Service" || service.RequestPrefix != "my-svc" {
		t.Fatalf("CreateService() = %+v, want tenant/name/prefix", service)
	}
	if !service.Enabled || service.PublishStatus != "draft" {
		t.Fatalf("CreateService() = %+v, want enabled/draft", service)
	}
}

func TestGetService(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, service := seedTenantAndService(t, store)

	byID, err := store.GetService(ctx, tenantID, service.ID)
	if err != nil {
		t.Fatalf("GetService(by id) error: %v", err)
	}
	byPrefix, err := store.GetServiceByPrefix(ctx, tenantID, service.RequestPrefix)
	if err != nil {
		t.Fatalf("GetServiceByPrefix() error: %v", err)
	}
	if byID.ID != byPrefix.ID {
		t.Fatalf("GetService() IDs = (%q,%q), want same", byID.ID, byPrefix.ID)
	}

	if _, err := store.GetService(ctx, tenantID, "nonexistent"); err != repository.ErrNotFound {
		t.Fatalf("GetService(nonexistent) error = %v, want %v", err, repository.ErrNotFound)
	}
}

func TestListServices(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, _ := seedTenantAndService(t, store)
	if _, err := store.CreateService(ctx, repository.CreateServiceParams{
		TenantID:      tenantID,
		Name:            "Svc 2",
		RequestPrefix: "svc-2",
		Enabled:       false,
	}); err != nil {
		t.Fatalf("CreateService() error: %v", err)
	}

	services, err := store.ListServices(ctx, tenantID, repository.ServiceFilter{})
	if err != nil {
		t.Fatalf("ListServices() error: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("ListServices() length = %d, want 2", len(services))
	}

	enabled := true
	active, err := store.ListServices(ctx, tenantID, repository.ServiceFilter{Enabled: &enabled})
	if err != nil {
		t.Fatalf("ListServices(enabled) error: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("ListServices(enabled) length = %d, want 1", len(active))
	}
}

func TestUpdateService(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, service := seedTenantAndService(t, store)

	name := "Updated"
	desc := "updated desc"
	enabled := false
	updated, err := store.UpdateService(ctx, tenantID, service.ID, repository.UpdateServiceParams{
		Name:        &name,
		Description: &desc,
		Enabled:     &enabled,
	})
	if err != nil {
		t.Fatalf("UpdateService() error: %v", err)
	}
	if updated.Name != name || updated.Description != desc || updated.Enabled != enabled {
		t.Fatalf("UpdateService() = %+v, want updated fields", updated)
	}
}

func TestCreateServiceVersion(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, service := seedTenantAndService(t, store)

	version, err := store.CreateServiceVersion(ctx, tenantID, repository.CreateServiceVersionParams{
		ServiceID: service.ID,
		Snapshot: repository.ServiceSnapshot{
			Name:            "v1",
			RequestPrefix:   service.RequestPrefix,
			DefaultProvider: service.DefaultProvider,
			Enabled:         service.Enabled,
		},
	})
	if err != nil {
		t.Fatalf("CreateServiceVersion() error: %v", err)
	}
	if version.ServiceID != service.ID || version.Version != 1 || version.Status != "draft" {
		t.Fatalf("CreateServiceVersion() = %+v, want serviceID/version/status", version)
	}
}

func TestListServiceVersions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, service := seedTenantAndService(t, store)

	for i := 0; i < 2; i++ {
		if _, err := store.CreateServiceVersion(ctx, tenantID, repository.CreateServiceVersionParams{
			ServiceID: service.ID,
		}); err != nil {
			t.Fatalf("CreateServiceVersion() error: %v", err)
		}
	}

	versions, err := store.ListServiceVersions(ctx, tenantID, service.ID)
	if err != nil {
		t.Fatalf("ListServiceVersions() error: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("ListServiceVersions() length = %d, want 2", len(versions))
	}
}

func TestPublishServiceVersion(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, service := seedTenantAndService(t, store)

	version, err := store.CreateServiceVersion(ctx, tenantID, repository.CreateServiceVersionParams{
		ServiceID: service.ID,
	})
	if err != nil {
		t.Fatalf("CreateServiceVersion() error: %v", err)
	}

	svc, ver, err := store.PublishServiceVersion(ctx, tenantID, service.ID, repository.PublishServiceVersionParams{
		VersionID: version.ID,
		Mode:      "published",
	})
	if err != nil {
		t.Fatalf("PublishServiceVersion() error: %v", err)
	}
	if svc.PublishStatus != "published" || svc.PublishedVersionID != version.ID {
		t.Fatalf("PublishServiceVersion() service = %+v, want published", svc)
	}
	if ver.Status != "published" {
		t.Fatalf("PublishServiceVersion() version = %+v, want published", ver)
	}
}

func TestPromoteStagedServiceVersion(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, service := seedTenantAndService(t, store)

	version, err := store.CreateServiceVersion(ctx, tenantID, repository.CreateServiceVersionParams{
		ServiceID: service.ID,
	})
	if err != nil {
		t.Fatalf("CreateServiceVersion() error: %v", err)
	}

	if _, _, err := store.PublishServiceVersion(ctx, tenantID, service.ID, repository.PublishServiceVersionParams{
		VersionID: version.ID,
		Mode:      "stage",
	}); err != nil {
		t.Fatalf("PublishServiceVersion(stage) error: %v", err)
	}

	svc, ver, err := store.PromoteStagedServiceVersion(ctx, tenantID, service.ID)
	if err != nil {
		t.Fatalf("PromoteStagedServiceVersion() error: %v", err)
	}
	if svc.PublishStatus != "published" || svc.StagedVersionID != "" {
		t.Fatalf("PromoteStagedServiceVersion() service = %+v, want published", svc)
	}
	if ver.Status != "published" {
		t.Fatalf("PromoteStagedServiceVersion() version = %+v, want published", ver)
	}
}

func TestRollbackServiceVersion(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, service := seedTenantAndService(t, store)

	v1, err := store.CreateServiceVersion(ctx, tenantID, repository.CreateServiceVersionParams{
		ServiceID: service.ID,
	})
	if err != nil {
		t.Fatalf("CreateServiceVersion(v1) error: %v", err)
	}
	if _, _, err := store.PublishServiceVersion(ctx, tenantID, service.ID, repository.PublishServiceVersionParams{
		VersionID: v1.ID,
		Mode:      "published",
	}); err != nil {
		t.Fatalf("PublishServiceVersion(v1) error: %v", err)
	}

	v2, err := store.CreateServiceVersion(ctx, tenantID, repository.CreateServiceVersionParams{
		ServiceID: service.ID,
	})
	if err != nil {
		t.Fatalf("CreateServiceVersion(v2) error: %v", err)
	}
	if _, _, err := store.PublishServiceVersion(ctx, tenantID, service.ID, repository.PublishServiceVersionParams{
		VersionID: v2.ID,
		Mode:      "published",
	}); err != nil {
		t.Fatalf("PublishServiceVersion(v2) error: %v", err)
	}

	svc, ver, err := store.RollbackServiceVersion(ctx, tenantID, service.ID, repository.RollbackServiceVersionParams{
		VersionID: v1.ID,
	})
	if err != nil {
		t.Fatalf("RollbackServiceVersion() error: %v", err)
	}
	if svc.PublishedVersionID != v1.ID {
		t.Fatalf("RollbackServiceVersion() service published = %q, want %q", svc.PublishedVersionID, v1.ID)
	}
	if ver.Status != "published" {
		t.Fatalf("RollbackServiceVersion() version = %+v, want published", ver)
	}
}
