package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

func TestUsageFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-uf", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodGet, "/admin/usage/summary?days=7&provider=test-openai&model=m&project_id=p1&user_id=u1&api_key_id=k1", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET usage summary with filter status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].(map[string]any)
	filter := data["filter"].(map[string]any)
	if filter["provider"] != "test-openai" || filter["model"] != "m" || filter["project_id"] != "p1" {
		t.Fatalf("usageFilter did not parse query params correctly: %#v", filter)
	}
}

func TestUsageFilterToResponse(t *testing.T) {
	now := time.Now().UTC()
	filter := repository.UsageFilter{
		TenantID:     "t1",
		ProjectID:    "p1",
		UserID:       "u1",
		APIKeyID:     "k1",
		ProviderName: "prov",
		Model:        "m1",
		StartTime:    now,
		EndTime:      now.Add(time.Hour),
	}
	resp := usageFilterToResponse(filter)
	if resp["tenant_id"] != "t1" || resp["provider"] != "prov" || resp["model"] != "m1" {
		t.Fatalf("usageFilterToResponse mismatch: %#v", resp)
	}
}

func TestResponseToResponse(t *testing.T) {
	now := time.Now().UTC()
	record := repository.ResponseRecord{
		ID:           "r1",
		TenantID:     "t1",
		ProjectID:    "p1",
		UserID:       "u1",
		APIKeyID:     "k1",
		ProviderName: "prov",
		Model:        "m1",
		Status:       "completed",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	resp := responseToResponse(record)
	if resp["id"] != "r1" || resp["status"] != "completed" || resp["provider_name"] != "prov" {
		t.Fatalf("responseToResponse mismatch: %#v", resp)
	}
}

func TestZeroTimeOrValue(t *testing.T) {
	if zeroTimeOrValue(time.Time{}) != nil {
		t.Fatal("zeroTimeOrValue(zero) should return nil")
	}
	now := time.Now().UTC()
	if zeroTimeOrValue(now) != now {
		t.Fatal("zeroTimeOrValue(non-zero) should return the value")
	}
}

func TestProviderConfigFromCreateRequest(t *testing.T) {
	req := CreateProviderRequest{Name: "p1", Model: "m1", RoutingWeight: 3, Type: "openai", BaseURL: "http://x", APIKey: "k"}
	cfg := providerConfigFromCreateRequest(req)
	if cfg.Name != "p1" || cfg.Model != "m1" || cfg.Weight != 3 {
		t.Fatalf("providerConfigFromCreateRequest mismatch: %+v", cfg)
	}
	req2 := CreateProviderRequest{Name: "p2", Model: "m2", RoutingWeight: 0}
	cfg2 := providerConfigFromCreateRequest(req2)
	if cfg2.Weight != 1 {
		t.Fatalf("providerConfigFromCreateRequest default weight = %d, want 1", cfg2.Weight)
	}
}

func TestMergeProviderUpdate(t *testing.T) {
	current := repository.ProviderRegistryRecord{Name: "p1", Type: "openai", Model: "m1", Enabled: true, RuntimeConfig: &repository.ProviderRuntimeConfig{Timeout: 5}}
	enabled := false
	model := "m2"
	weight := 7
	req := UpdateProviderRequest{Enabled: &enabled, Model: &model, RoutingWeight: &weight}
	updated := mergeProviderUpdate(current, req)
	if updated.Model != "m2" || updated.Enabled != false || updated.RoutingWeight != 7 {
		t.Fatalf("mergeProviderUpdate mismatch: model=%s enabled=%v weight=%d", updated.Model, updated.Enabled, updated.RoutingWeight)
	}
}

func TestApplyProviderCapabilityOverrides(t *testing.T) {
	record := repository.ProviderRegistryRecord{SupportsChat: false, SupportsStream: false}
	chat := true
	applyProviderCapabilityOverrides(&record, providerCapabilityOverrides{SupportsChat: &chat})
	if !record.SupportsChat {
		t.Fatal("capability override for SupportsChat did not apply")
	}
}

func TestScopeTenantID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})

	// Test via handler endpoint: super admin with tenant_id query
	superToken := seedAdminToken(t, env, repository.RoleSuperAdmin, "super-scope", "secret").APIKey + ":" + "secret"
	rec := performJSONRequest(t, env, http.MethodGet, "/admin/usage/summary?tenant_id=tenant-a", superToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("super admin scoped usage summary status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Test via handler endpoint: tenant admin without tenant_id query
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-scope", "secret").APIKey + ":" + "secret"
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/usage/summary", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("tenant admin scoped usage summary status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestResolveTargetTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	superToken := seedAdminToken(t, env, repository.RoleSuperAdmin, "super-resolve", "secret").APIKey + ":" + "secret"

	// super admin must provide tenant_id when creating user
	rec := performJSONRequest(t, env, http.MethodPost, "/admin/users", superToken, `{"name":"missing-tenant"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("super admin missing tenant_id status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	// super admin with tenant_id succeeds
	rec = performJSONRequest(t, env, http.MethodPost, "/admin/users", superToken, `{"tenant_id":"tenant-a","name":"has-tenant","email":"h@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("super admin with tenant_id status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestAllProvidersExist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	superToken := seedAdminToken(t, env, repository.RoleSuperAdmin, "super-exist", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/tenants/tenant-a/providers", superToken, `{"providers":["missing-provider"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown provider status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/tenants/tenant-a/providers", superToken, `{"providers":["test-openai"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("existing provider status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAppendTenantProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-append", "secret").APIKey + ":" + "secret"

	// Create a new runtime provider which auto-appends to tenant
	rec := performJSONRequest(t, env, http.MethodPost, "/admin/providers", adminToken, `{"name":"append-provider","type":"openai","model":"m1","enabled":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST provider status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
}

func TestRemoveTenantProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-remove", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodDelete, "/admin/providers/test-openai", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE provider status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}
