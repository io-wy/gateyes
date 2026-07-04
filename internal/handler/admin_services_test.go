package handler

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

func TestUpdateService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-svc-upd", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/services", adminToken, `{"name":"Svc","request_prefix":"svc","default_provider":"test-openai","default_model":"provider-model","enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST service status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	serviceID := decodeBodyMap(t, rec)["service"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/services/"+serviceID, adminToken, `{"name":"Svc Updated","enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT service status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)
	if data["name"] != "Svc Updated" || data["enabled"] != false {
		t.Fatalf("PUT service payload = %#v, want updated name and enabled", data)
	}

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/services/missing", adminToken, `{"name":"X"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT missing service status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestDeleteService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-svc-del", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/services", adminToken, `{"name":"DelSvc","request_prefix":"delsvc","default_provider":"test-openai","default_model":"provider-model","enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST service status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	serviceID := decodeBodyMap(t, rec)["service"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/services/"+serviceID, adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE service status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)
	if data["deleted"] != true {
		t.Fatalf("DELETE service payload = %#v, want deleted=true", data)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/services/"+serviceID, adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET deleted service status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/services/missing", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing service status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestListServiceVersions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-svc-ver", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/services", adminToken, `{"name":"VerSvc","request_prefix":"versvc","default_provider":"test-openai","default_model":"provider-model","enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST service status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	serviceID := decodeBodyMap(t, rec)["service"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/services/"+serviceID+"/versions", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET versions status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].([]any)
	if len(data) == 0 {
		t.Fatal("expected at least one version")
	}
}

func TestCreateServiceVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-svc-cv", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/services", adminToken, `{"name":"CvSvc","request_prefix":"cvsvc","default_provider":"test-openai","default_model":"provider-model","enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST service status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	serviceID := decodeBodyMap(t, rec)["service"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/services/"+serviceID+"/versions", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST version status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)
	if data["id"] == "" {
		t.Fatal("expected version id in response")
	}

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/services/missing/versions", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST version missing service status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestRollbackServiceVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-svc-rb", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/services", adminToken, `{"name":"RbSvc","request_prefix":"rbsvc","default_provider":"test-openai","default_model":"provider-model","enabled":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST service status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	serviceID := decodeBodyMap(t, rec)["service"].(map[string]any)["id"].(string)
	versionID := decodeBodyMap(t, rec)["initial_version"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/services/"+serviceID+"/rollback", adminToken, `{"version_id":"`+versionID+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST rollback status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)
	if data["service"] == nil || data["version"] == nil {
		t.Fatalf("expected service and version in rollback response: %#v", data)
	}

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/services/missing/rollback", adminToken, `{"version_id":"v1"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST rollback missing service status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
