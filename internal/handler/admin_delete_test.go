package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

func TestDeleteProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat"})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-del-prov", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodDelete, "/admin/providers/test-openai", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE provider status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/providers/missing", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing provider status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestDeleteTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	superToken := seedAdminToken(t, env, repository.RoleSuperAdmin, "super-del-ten", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/tenants", superToken, `{"id":"del-ten","slug":"del-ten","name":"To Delete"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tenant: %d %s", rec.Code, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/tenants/del-ten", superToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE tenant status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/tenants/missing", superToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing tenant status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestDeleteProject(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-del-proj2", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/projects", adminToken, `{"slug":"del-proj2","name":"To Delete"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", rec.Code, rec.Body.String())
	}
	projectID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/projects/"+projectID, adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE project status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/projects/missing", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing project status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestDeleteUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-del-user", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/users", adminToken, `{"name":"del-user","email":"del@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", rec.Code, rec.Body.String())
	}
	userID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/users/"+userID, adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE user status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/users/missing", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing user status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
