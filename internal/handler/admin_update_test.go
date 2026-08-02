package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

func TestUpdateProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-upd-prov", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPut, "/admin/providers/test-openai", adminToken, `{"model":"updated-model","routing_weight":9}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT provider status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)
	if data["model"] != "updated-model" || data["routing_weight"] != float64(9) {
		t.Fatalf("PUT provider payload = %#v, want updated model and weight", data)
	}

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/providers/missing", adminToken, `{"model":"x"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT missing provider status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestSyncRouter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-sync-router", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/v1/sync/router", adminToken, `{
		"strategy":"least_gpu_cache",
		"ruleEngine":{"enabled":true,"rules":[{"name":"qwen","match":{"models":["qwen"]},"action":{"providers":["test-openai"]}}]}
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST sync router status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)
	if data["strategy"] != "least_gpu_cache" || data["rule_count"] != float64(1) {
		t.Fatalf("POST sync router payload = %#v, want strategy and rule count", data)
	}

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/v1/sync/router", adminToken, `{"strategy":"bad"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST sync router invalid status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestSyncBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-sync-budget", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/projects", adminToken, `{"slug":"budget-proj","name":"Budget Project"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST project status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	projectID := decodeBodyMap(t, rec)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/v1/sync/budget", adminToken, `{
		"subject_kind":"project",
		"subject_name":"`+projectID+`",
		"budget_usd":42.5,
		"budget_policy":"soft_alert",
		"rate_limit_qps":8
	}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST sync budget status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	project, err := env.store.GetProject(context.Background(), "tenant-a", projectID)
	if err != nil {
		t.Fatalf("GetProject() error: %v", err)
	}
	if project.BudgetUSD != 42.5 || project.BudgetPolicy != "soft_alert" {
		t.Fatalf("project budget = %+v, want synced budget", project)
	}

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/v1/sync/budget", adminToken, `{"subject_kind":"service","subject_name":"svc"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST sync budget unsupported status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestUpdateTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	superToken := seedAdminToken(t, env, repository.RoleSuperAdmin, "super-upd-ten", "secret").APIKey + ":" + "secret"

	// Create a new tenant to update so we don't inactivate the seeded one
	rec := performJSONRequest(t, env, http.MethodPost, "/admin/tenants", superToken, `{"id":"upd-ten","slug":"upd-ten","name":"Upd Tenant"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST tenant status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/tenants/upd-ten", superToken, `{"name":"Updated Tenant","status":"inactive"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT tenant status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)
	if data["name"] != "Updated Tenant" || data["status"] != "inactive" {
		t.Fatalf("PUT tenant payload = %#v, want updated name and status", data)
	}

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/tenants/missing", superToken, `{"name":"X"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT missing tenant status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestUpdateProject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-upd-proj", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/projects", adminToken, `{"slug":"upd-proj","name":"Upd Proj"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST project status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	projectID := decodeBodyMap(t, rec)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/projects/"+projectID, adminToken, `{"name":"Upd Proj New","budget_usd":99}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT project status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)
	if data["name"] != "Upd Proj New" || data["budget_usd"] != float64(99) {
		t.Fatalf("PUT project payload = %#v, want updated name and budget", data)
	}

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/projects/missing", adminToken, `{"name":"X"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT missing project status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestUpdateUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-upd-user", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/users", adminToken, `{"name":"upd-user","email":"u@example.com"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST user status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	userID := decodeBodyMap(t, rec)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/users/"+userID, adminToken, `{"quota":50,"qps":5,"status":"inactive"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT user status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)
	if data["quota"] != float64(50) || data["qps"] != float64(5) || data["status"] != "inactive" {
		t.Fatalf("PUT user payload = %#v, want updated quota, qps, status", data)
	}

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/users/missing", adminToken, `{"status":"inactive"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT missing user status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestUpdateAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-upd-key", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/users", adminToken, `{"name":"key-owner","email":"k@example.com"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST user status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	userID := decodeBodyMap(t, rec)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/keys", adminToken, `{"user_id":"`+userID+`","budget_usd":10}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST key status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	keyID := decodeBodyMap(t, rec)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/keys/"+keyID, adminToken, `{"budget_usd":20,"rate_limit_qps":5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT key status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)
	if data["budget_usd"] != float64(20) || data["rate_limit_qps"] != float64(5) {
		t.Fatalf("PUT key payload = %#v, want updated budget and qps", data)
	}

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/keys/missing", adminToken, `{"budget_usd":1}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT missing key status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
