package sqlstore

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
)

func seedTenantAndServiceForSub(t *testing.T, store *Store) (string, *repository.ServiceRecord) {
	t.Helper()
	ctx := context.Background()

	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:   "tenant-sub",
		Slug: "tenant-sub",
		Name: "Tenant Sub",
	})
	if err != nil {
		t.Fatalf("EnsureTenant() error: %v", err)
	}

	service, err := store.CreateService(ctx, repository.CreateServiceParams{
		TenantID:      tenant.ID,
		Name:          "Sub Service",
		RequestPrefix: "sub-svc",
		Enabled:       true,
	})
	if err != nil {
		t.Fatalf("CreateService() error: %v", err)
	}
	return tenant.ID, service
}

func TestCreateServiceSubscription(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, service := seedTenantAndServiceForSub(t, store)
	sub, err := store.CreateServiceSubscription(ctx, tenantID, repository.CreateServiceSubscriptionParams{
		ServiceID:             service.ID,
		ConsumerName:          "Alice",
		ConsumerEmail:         "alice@example.com",
		ConsumerUserID:        "user-alice",
		RequestedBudgetUSD:    100,
		RequestedRateLimitQPS: 10,
		AllowedSurfaces:       []string{"responses"},
	})
	if err != nil {
		t.Fatalf("CreateServiceSubscription() error: %v", err)
	}
	if sub.TenantID != tenantID || sub.Status != "pending" || sub.ConsumerName != "Alice" {
		t.Fatalf("CreateServiceSubscription() = %+v, want tenant/status/name", sub)
	}
}

func TestListServiceSubscriptions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, service := seedTenantAndServiceForSub(t, store)
	for _, name := range []string{"alice", "bob"} {
		if _, err := store.CreateServiceSubscription(ctx, tenantID, repository.CreateServiceSubscriptionParams{
			ServiceID:      service.ID,
			ConsumerName:   name,
			ConsumerEmail:  name + "@example.com",
			ConsumerUserID: "user-" + name,
		}); err != nil {
			t.Fatalf("CreateServiceSubscription(%s) error: %v", name, err)
		}
	}

	subs, err := store.ListServiceSubscriptions(ctx, tenantID, repository.ServiceSubscriptionFilter{})
	if err != nil {
		t.Fatalf("ListServiceSubscriptions() error: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("ListServiceSubscriptions() length = %d, want 2", len(subs))
	}

	bySvc, err := store.ListServiceSubscriptions(ctx, tenantID, repository.ServiceSubscriptionFilter{ServiceID: service.ID})
	if err != nil {
		t.Fatalf("ListServiceSubscriptions(by service) error: %v", err)
	}
	if len(bySvc) != 2 {
		t.Fatalf("ListServiceSubscriptions(by service) length = %d, want 2", len(bySvc))
	}
}

func TestGetServiceSubscription(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, service := seedTenantAndServiceForSub(t, store)
	sub, err := store.CreateServiceSubscription(ctx, tenantID, repository.CreateServiceSubscriptionParams{
		ServiceID:      service.ID,
		ConsumerName:   "charlie",
		ConsumerEmail:  "charlie@example.com",
		ConsumerUserID: "user-charlie",
	})
	if err != nil {
		t.Fatalf("CreateServiceSubscription() error: %v", err)
	}

	got, err := store.GetServiceSubscription(ctx, tenantID, sub.ID)
	if err != nil {
		t.Fatalf("GetServiceSubscription() error: %v", err)
	}
	if got.ID != sub.ID || got.ConsumerName != "charlie" {
		t.Fatalf("GetServiceSubscription() = %+v, want id/name", got)
	}

	if _, err := store.GetServiceSubscription(ctx, tenantID, "nonexistent"); err != repository.ErrNotFound {
		t.Fatalf("GetServiceSubscription(nonexistent) error = %v, want %v", err, repository.ErrNotFound)
	}
}

func TestUpdateServiceSubscription(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tenantID, service := seedTenantAndServiceForSub(t, store)
	sub, err := store.CreateServiceSubscription(ctx, tenantID, repository.CreateServiceSubscriptionParams{
		ServiceID:      service.ID,
		ConsumerName:   "dave",
		ConsumerEmail:  "dave@example.com",
		ConsumerUserID: "user-dave",
	})
	if err != nil {
		t.Fatalf("CreateServiceSubscription() error: %v", err)
	}

	status := "approved"
	note := "looks good"
	updated, err := store.UpdateServiceSubscription(ctx, tenantID, sub.ID, repository.UpdateServiceSubscriptionParams{
		Status:     &status,
		ReviewNote: &note,
	})
	if err != nil {
		t.Fatalf("UpdateServiceSubscription() error: %v", err)
	}
	if updated.Status != status || updated.ReviewNote != note {
		t.Fatalf("UpdateServiceSubscription() = %+v, want updated fields", updated)
	}
	if updated.ApprovedAt == nil {
		t.Fatalf("UpdateServiceSubscription() approved_at not set")
	}
}
