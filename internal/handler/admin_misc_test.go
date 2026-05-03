package handler

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

func TestGetProjectUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-proj-use", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/projects", adminToken, `{"slug":"proj-use","name":"Proj Use"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST project status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	projectID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/projects/"+projectID+"/usage", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET project usage status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].(map[string]any)
	if data["project"] == nil {
		t.Fatal("expected project in response")
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/projects/missing/usage", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing project usage status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestGetResponseTrace_WithTrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-trace", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodGet, "/admin/responses/resp-1/trace", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing trace status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestReloadConfig_NotConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-reload", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/reload", adminToken, "")
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("POST reload status = %d, want %d: %s", rec.Code, http.StatusNotImplemented, rec.Body.String())
	}
}

func TestDeleteVirtualKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-del-vk", "secret").APIKey + ":" + "secret"

	// List existing API keys to get an api_key_id for virtual key creation
	rec := performJSONRequest(t, env, http.MethodGet, "/admin/keys", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET keys status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	keys := decodeBodyMap(t, rec)["data"].([]any)
	if len(keys) == 0 {
		t.Fatal("need at least one api key")
	}
	apiKeyID := keys[0].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/virtual-keys", adminToken, `{"user_id":"u1","api_key_id":"`+apiKeyID+`","name":"vk-del"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST virtual key status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	vkID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/virtual-keys/"+vkID, adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE virtual key status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if decodeBodyMap(t, rec)["data"].(map[string]any)["deleted"] != true {
		t.Fatal("expected deleted=true")
	}

	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/virtual-keys/missing", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing virtual key status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestGetAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-get-key", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/users", adminToken, `{"name":"key-user","email":"k@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST user status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	userID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/keys", adminToken, `{"user_id":"`+userID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST key status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	keyID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/keys/"+keyID, adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET key status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].(map[string]any)
	if data["id"] != keyID {
		t.Fatalf("GET key payload = %#v, want id=%s", data, keyID)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/keys/missing", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing key status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestRevokeAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-revoke", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/users", adminToken, `{"name":"revoke-user","email":"r@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST user status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	userID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/keys", adminToken, `{"user_id":"`+userID+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST key status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	keyID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/keys/"+keyID+"/revoke", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST revoke status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].(map[string]any)
	if data["status"] != repository.StatusRevoked {
		t.Fatalf("expected revoked status, got %v", data["status"])
	}
}

func TestGetService_WithVersions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-get-svc", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/services", adminToken, `{"name":"GetSvc","request_prefix":"getsvc","default_provider":"test-openai","default_model":"provider-model","enabled":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST service status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	serviceID := decodeBodyMap(t, rec)["data"].(map[string]any)["service"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/services/"+serviceID, adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET service status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].(map[string]any)
	if data["service"] == nil || data["versions"] == nil {
		t.Fatalf("expected service and versions, got %#v", data)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/services/missing", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing service status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestGetServiceSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-get-sub", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/services", adminToken, `{"name":"SubSvc","request_prefix":"subsvc","default_provider":"test-openai","default_model":"provider-model","enabled":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST service status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	serviceID := decodeBodyMap(t, rec)["data"].(map[string]any)["service"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/services/"+serviceID+"/subscriptions", adminToken, `{"consumer_name":"c1"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST subscription status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	subID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/subscriptions/"+subID, adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET subscription status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].(map[string]any)
	if data["id"] != subID {
		t.Fatalf("GET subscription payload = %#v, want id=%s", data, subID)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/subscriptions/missing", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing subscription status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
