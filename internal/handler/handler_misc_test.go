package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestValidProviderHealthStatus(t *testing.T) {
	if !validProviderHealthStatus(provider.ProviderHealthHealthy) {
		t.Fatal("expected healthy to be valid")
	}
	if !validProviderHealthStatus(provider.ProviderHealthDegraded) {
		t.Fatal("expected degraded to be valid")
	}
	if !validProviderHealthStatus(provider.ProviderHealthUnhealthy) {
		t.Fatal("expected unhealthy to be valid")
	}
	if validProviderHealthStatus("unknown") {
		t.Fatal("expected unknown to be invalid")
	}
}

func TestValidEntityStatus(t *testing.T) {
	if !validEntityStatus(repository.StatusActive) {
		t.Fatal("expected active to be valid")
	}
	if !validEntityStatus(repository.StatusInactive) {
		t.Fatal("expected inactive to be valid")
	}
	if !validEntityStatus(repository.StatusRevoked) {
		t.Fatal("expected revoked to be valid")
	}
	if validEntityStatus("unknown") {
		t.Fatal("expected unknown to be invalid")
	}
}

func TestMetricsExporter(t *testing.T) {
	m := NewMetrics("test_exporter")
	stats := provider.NewStats()
	stats.RecordRequest("test-openai", true, 5, 20)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	m.StartProviderStatsExporter(ctx, stats, 10*time.Millisecond)
}

func TestMetricsDisabled(t *testing.T) {
	m := NewMetricsFromConfig(config.MetricsConfig{Enabled: false})
	m.TrackInFlight("responses")()
	m.TrackStream("responses")()
	m.ObserveTTFT("responses", "p", time.Millisecond)
	m.ObserveStreamDuration("responses", "p", "success", time.Millisecond)
	m.RecordSuccess("responses", "p", provider.Usage{}, time.Millisecond, nil, 0, 0)
	m.RecordError("responses", "p", "error", "class")
	m.SetCircuitBreakerState("t", "p", 1)
	m.RecordCacheLookup("l1", "hit")
	m.RecordCacheWrite("l1", "ok")
	m.ObserveCacheValueSize("l1", 100)
	m.ObserveCacheGetDuration("l1", time.Millisecond)
	if m.Handler() == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestWriteSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	if err := writeSSE(c, map[string]any{"ok": true}); err != nil {
		t.Fatalf("writeSSE error: %v", err)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"ok":true`)) {
		t.Fatalf("expected ok in SSE body, got %q", rec.Body.String())
	}
	writeSSEDone(c)
	if !bytes.Contains(rec.Body.Bytes(), []byte("[DONE]")) {
		t.Fatalf("expected DONE marker, got %q", rec.Body.String())
	}
}

func TestUpdateAPIKey_InvalidStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-upd-key-bad", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/users", adminToken, `{"name":"key-user-bad","email":"k@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST user status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	userID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/keys", adminToken, `{"user_id":"`+userID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST key status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	keyID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/keys/"+keyID, adminToken, `{"status":"bad_status"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT key invalid status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetTenant_WithProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	superToken := seedAdminToken(t, env, repository.RoleSuperAdmin, "super-get-ten", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodGet, "/admin/tenants/tenant-a", superToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET tenant status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].(map[string]any)
	if data["tenant"] == nil || data["providers"] == nil {
		t.Fatalf("expected tenant and providers, got %#v", data)
	}
}

func TestGetService_Missing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-get-svc-miss", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodGet, "/admin/services/missing", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing service status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestListServiceVersions_MissingService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-lsv-miss", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodGet, "/admin/services/missing/versions", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET versions missing service status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
