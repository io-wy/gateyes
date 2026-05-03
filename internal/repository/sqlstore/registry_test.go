package sqlstore

import (
	"context"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/repository"
)

func TestUpsertProviderRegistry(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	record := repository.ProviderRegistryRecord{
		Name:          "upsert-provider",
		Type:          "openai",
		Vendor:        "vllm",
		BaseURL:       "https://api.example.com/v1",
		Endpoint:      "responses",
		Model:         "gpt-test",
		Enabled:       true,
		HealthStatus:  "healthy",
		RoutingWeight: 5,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if err := store.UpsertProviderRegistry(ctx, record); err != nil {
		t.Fatalf("UpsertProviderRegistry(create) error: %v", err)
	}

	got, err := store.GetProviderRegistry(ctx, record.Name)
	if err != nil {
		t.Fatalf("GetProviderRegistry() error: %v", err)
	}
	if got.RoutingWeight != 5 {
		t.Fatalf("GetProviderRegistry() weight = %d, want 5", got.RoutingWeight)
	}

	record.RoutingWeight = 8
	if err := store.UpsertProviderRegistry(ctx, record); err != nil {
		t.Fatalf("UpsertProviderRegistry(update) error: %v", err)
	}
	got, err = store.GetProviderRegistry(ctx, record.Name)
	if err != nil {
		t.Fatalf("GetProviderRegistry() error: %v", err)
	}
	if got.RoutingWeight != 8 {
		t.Fatalf("GetProviderRegistry() weight = %d, want 8", got.RoutingWeight)
	}
}

func TestListProviderRegistry(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"p1", "p2"} {
		if err := store.UpsertProviderRegistry(ctx, repository.ProviderRegistryRecord{
			Name:      name,
			Type:      "openai",
			BaseURL:   "https://" + name + ".com",
			Enabled:   true,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("UpsertProviderRegistry(%s) error: %v", name, err)
		}
	}

	all, err := store.ListProviderRegistry(ctx)
	if err != nil {
		t.Fatalf("ListProviderRegistry() error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListProviderRegistry() length = %d, want 2", len(all))
	}
}

func TestUpdateProviderRegistry(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.UpsertProviderRegistry(ctx, repository.ProviderRegistryRecord{
		Name:          "upd-provider",
		Type:          "openai",
		BaseURL:       "https://api.example.com/v1",
		Enabled:       true,
		Drain:         false,
		HealthStatus:  "healthy",
		RoutingWeight: 3,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertProviderRegistry() error: %v", err)
	}

	drain := true
	health := "unhealthy"
	weight := 9
	updated, err := store.UpdateProviderRegistry(ctx, "upd-provider", repository.UpdateProviderRegistryParams{
		Drain:         &drain,
		HealthStatus:  &health,
		RoutingWeight: &weight,
	})
	if err != nil {
		t.Fatalf("UpdateProviderRegistry() error: %v", err)
	}
	if !updated.Drain || updated.HealthStatus != health || updated.RoutingWeight != weight {
		t.Fatalf("UpdateProviderRegistry() = %+v, want updated fields", updated)
	}
}

func TestDeleteProviderRegistry(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.UpsertProviderRegistry(ctx, repository.ProviderRegistryRecord{
		Name:      "del-provider",
		Type:      "openai",
		BaseURL:   "https://api.example.com/v1",
		Enabled:   true,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertProviderRegistry() error: %v", err)
	}

	if err := store.DeleteProviderRegistry(ctx, "del-provider"); err != nil {
		t.Fatalf("DeleteProviderRegistry() error: %v", err)
	}
	if _, err := store.GetProviderRegistry(ctx, "del-provider"); err != repository.ErrNotFound {
		t.Fatalf("GetProviderRegistry(after delete) error = %v, want %v", err, repository.ErrNotFound)
	}

	if err := store.DeleteProviderRegistry(ctx, "nonexistent"); err != repository.ErrNotFound {
		t.Fatalf("DeleteProviderRegistry(nonexistent) error = %v, want %v", err, repository.ErrNotFound)
	}
}
