package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
)

func TestHealthCheckerUpdatesRegistryStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer upstream.Close()

	database, err := db.Open(config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "health.db"),
		AutoMigrate:            true,
		MaxOpenConns:           1,
		MaxIdleConns:           1,
		ConnMaxLifetimeSeconds: 60,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	store := sqlstore.New(database)
	manager, err := NewManager([]config.ProviderConfig{{
		Name:      "openai-bad",
		Type:      "openai",
		BaseURL:   upstream.URL,
		Endpoint:  "chat",
		APIKey:    "upstream-key",
		Model:     "provider-model",
		Timeout:   5,
		Enabled:   true,
		MaxTokens: 256,
	}})
	if err != nil {
		t.Fatalf("NewManager() error: %v", err)
	}
	if err := store.UpsertProviderRegistry(context.Background(), repository.ProviderRegistryRecord{
		Name:              "openai-bad",
		Enabled:           true,
		HealthStatus:      ProviderHealthHealthy,
		RoutingWeight:     1,
		SupportsChat:      true,
		SupportsResponses: true,
		SupportsMessages:  true,
		SupportsStream:    true,
		SupportsTools:     true,
		SupportsImages:    true,
	}); err != nil {
		t.Fatalf("UpsertProviderRegistry() error: %v", err)
	}
	if records, err := store.ListProviderRegistry(context.Background()); err != nil {
		t.Fatalf("ListProviderRegistry() error: %v", err)
	} else {
		manager.ApplyRegistry(records)
	}

	checker := NewHealthChecker(config.HealthCheckConfig{
		Enabled:          true,
		TimeoutSeconds:   5,
		FailureThreshold: 1,
	}, store, manager, nil)

	if err := checker.ForceCheck(context.Background()); err != nil {
		t.Fatalf("ForceCheck() error: %v", err)
	}

	record, err := store.GetProviderRegistry(context.Background(), "openai-bad")
	if err != nil {
		t.Fatalf("GetProviderRegistry() error: %v", err)
	}
	if record.HealthStatus != ProviderHealthUnhealthy {
		t.Fatalf("provider health_status = %q, want %q", record.HealthStatus, ProviderHealthUnhealthy)
	}
	stats, ok := manager.Stats.Get("openai-bad")
	if !ok || stats.Status != ProviderHealthUnhealthy {
		t.Fatalf("provider stats = %+v, want unhealthy status", stats)
	}
}
