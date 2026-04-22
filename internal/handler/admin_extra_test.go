package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

func seedAdminToken(t *testing.T, env *handlerTestEnv, role string, key string, secret string) *repository.AuthIdentity {
	t.Helper()

	ctx := context.Background()
	tenant, err := env.store.GetTenant(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("GetTenant(tenant-a) error: %v", err)
	}
	if err := env.store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenant.ID,
		Key:        key,
		SecretHash: repository.HashSecret(secret),
		Name:       key,
		Email:      key + "@example.com",
		Role:       role,
		Quota:      100000,
		QPS:        100,
	}); err != nil {
		t.Fatalf("EnsureBootstrapKey(%s) error: %v", key, err)
	}
	identity, err := env.store.Authenticate(ctx, key)
	if err != nil {
		t.Fatalf("Authenticate(%s) error: %v", key, err)
	}
	return identity
}

func performJSONRequest(t *testing.T, env *handlerTestEnv, method string, path string, token string, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reqBody *bytes.Buffer
	if body == "" {
		reqBody = bytes.NewBuffer(nil)
	} else {
		reqBody = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, reqBody)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	env.server.engine.ServeHTTP(rec, req)
	return rec
}

func decodeBodyMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(%q) error: %v", rec.Body.String(), err)
	}
	return payload
}

func TestAdminUserLifecycleAndDashboardEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-upstream",
			"object":  "chat.completion",
			"created": 1700000000,
			"model":   "provider-model",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "admin hello",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     3,
				"completion_tokens": 2,
				"total_tokens":      5,
			},
		})
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
	})
	adminIdentity := seedAdminToken(t, env, repository.RoleTenantAdmin, "tenant-admin", "tenant-admin-secret")
	token := "tenant-admin:tenant-admin-secret"

	now := time.Now().UTC()
	if err := env.store.CreateUsageRecord(context.Background(), repository.UsageRecord{
		ID:               "usage-admin-1",
		TenantID:         adminIdentity.TenantID,
		UserID:           adminIdentity.UserID,
		APIKeyID:         adminIdentity.APIKeyID,
		ProviderName:     "test-openai",
		Model:            "public-model",
		PromptTokens:     3,
		CompletionTokens: 2,
		TotalTokens:      5,
		LatencyMs:        20,
		Status:           "success",
		CreatedAt:        now,
	}); err != nil {
		t.Fatalf("CreateUsageRecord() error: %v", err)
	}
	env.providerMgr.Stats.RecordRequest("test-openai", true, 5, 20)

	rec := performJSONRequest(t, env, http.MethodGet, "/admin/providers", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/providers status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/providers/test-openai", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/providers/test-openai status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/providers/test-openai/stats", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/providers/test-openai/stats status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPut, "/admin/providers/test-openai", token, `{"drain":true,"health_status":"degraded","routing_weight":9,"supports_images":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /admin/providers/test-openai status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	updatedProvider := decodeBodyMap(t, rec)["data"].(map[string]any)
	if updatedProvider["drain"] != true || updatedProvider["health_status"] != "degraded" || updatedProvider["routing_weight"] != float64(9) || updatedProvider["supports_images"] != false {
		t.Fatalf("PUT /admin/providers/test-openai payload = %#v, want updated registry metadata", updatedProvider)
	}

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/projects", token, `{"slug":"proj-a","name":"Project A","budget_usd":50}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/projects status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	projectPayload := decodeBodyMap(t, rec)["data"].(map[string]any)
	projectID := projectPayload["id"].(string)
	if projectPayload["slug"] != "proj-a" || projectPayload["budget_usd"] != float64(50) {
		t.Fatalf("POST /admin/projects payload = %#v, want project fields", projectPayload)
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/projects", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/projects status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/projects/"+projectID, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/projects/:id status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPut, "/admin/projects/"+projectID, token, `{"budget_usd":80}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /admin/projects/:id status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/users", token, `{"name":"bob","email":"bob@example.com","project_id":"`+projectID+`","key_budget_usd":12.5,"models":["gpt-b"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/users status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	createPayload := decodeBodyMap(t, rec)
	data := createPayload["data"].(map[string]any)
	userID := data["id"].(string)
	userAPIKey := data["api_key"].(string)
	userSecret := data["api_secret"].(string)
	if data["role"] != repository.RoleTenantUser {
		t.Fatalf("POST /admin/users role = %v, want %q", data["role"], repository.RoleTenantUser)
	}
	if data["project_id"] != projectID || data["key_budget_usd"] != float64(12.5) {
		t.Fatalf("POST /admin/users project/budget = %#v, want project_id and key_budget_usd", data)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/users", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/users status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/users/"+userID, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/users/:id status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/users/"+userID, token, `{"quota":50,"qps":9,"status":"inactive","models":["claude-b"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /admin/users/:id status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	userRecord, err := env.store.GetUser(context.Background(), adminIdentity.TenantID, userID)
	if err != nil {
		t.Fatalf("GetUser(created user) error: %v", err)
	}
	if ok, err := env.store.ConsumeQuota(context.Background(), userRecord.ID, 10); err != nil || !ok {
		t.Fatalf("ConsumeQuota(created user) = (%v,%v), want (true,nil)", ok, err)
	}
	if err := env.store.CreateUsageRecord(context.Background(), repository.UsageRecord{
		ID:               "usage-user-1",
		TenantID:         adminIdentity.TenantID,
		UserID:           userRecord.ID,
		APIKeyID:         userRecord.APIKey,
		ProviderName:     "test-openai",
		Model:            "public-model",
		PromptTokens:     1,
		CompletionTokens: 1,
		TotalTokens:      2,
		LatencyMs:        15,
		Status:           "success",
		CreatedAt:        now,
	}); err != nil {
		t.Fatalf("CreateUsageRecord(user) error: %v", err)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/users/"+userID+"/usage", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/users/:id/usage status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPost, "/admin/users/"+userID+"/reset", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/users/:id/reset status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/dashboard", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/dashboard status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	userToken := userAPIKey + ":" + userSecret
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/users", userToken, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /admin/users with tenant user token status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/users/"+userID, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /admin/users/:id status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestSuperAdminTenantRoutesAndPublicEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"provider-model\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"provider-model\",\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":2,\"total_tokens\":3}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
	})
	superIdentity := seedAdminToken(t, env, repository.RoleSuperAdmin, "super-admin", "super-admin-secret")
	token := "super-admin:super-admin-secret"

	rec := performJSONRequest(t, env, http.MethodGet, "/health", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /health status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/ready", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ready status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/metrics", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/v1/models", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/models status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/tenants", token, `{"id":"tenant-z","slug":"tenant-z","name":"Tenant Z"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/tenants status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/tenants", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/tenants status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/tenants/tenant-z", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/tenants/:id status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPut, "/admin/tenants/tenant-z", token, `{"name":"Tenant Z Updated","status":"inactive"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /admin/tenants/:id status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPost, "/admin/tenants/tenant-z/providers", token, `{"providers":["test-openai"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/tenants/:id/providers status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	if err := env.store.CreateResponse(context.Background(), repository.ResponseRecord{
		ID:           "resp-status",
		TenantID:     superIdentity.TenantID,
		UserID:       superIdentity.UserID,
		APIKeyID:     superIdentity.APIKeyID,
		ProviderName: "test-openai",
		Model:        "public-model",
		Status:       "in_progress",
	}); err != nil {
		t.Fatalf("CreateResponse(resp-status) error: %v", err)
	}
	if err := env.store.CreateResponse(context.Background(), repository.ResponseRecord{
		ID:           "resp-body",
		TenantID:     superIdentity.TenantID,
		UserID:       superIdentity.UserID,
		APIKeyID:     superIdentity.APIKeyID,
		ProviderName: "test-openai",
		Model:        "public-model",
		Status:       "completed",
		ResponseBody: []byte(`{"id":"resp-body","status":"completed"}`),
	}); err != nil {
		t.Fatalf("CreateResponse(resp-body) error: %v", err)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/v1/responses/resp-status", token, "")
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"status":"in_progress"`)) {
		t.Fatalf("GET /v1/responses/resp-status = (%d,%s), want status-only payload", rec.Code, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/v1/responses/resp-body", token, "")
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"id":"resp-body"`)) {
		t.Fatalf("GET /v1/responses/resp-body = (%d,%s), want stored response body", rec.Code, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/v1/responses/missing", token, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /v1/responses/missing status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}

	streamReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"stream":true}`))
	streamReq.Header.Set("Authorization", "Bearer "+token)
	streamReq.Header.Set("Content-Type", "application/json")
	streamRec := httptest.NewRecorder()
	env.server.engine.ServeHTTP(streamRec, streamReq)
	if streamRec.Code != http.StatusOK {
		t.Fatalf("POST /v1/chat/completions(stream) status = %d, want %d: %s", streamRec.Code, http.StatusOK, streamRec.Body.String())
	}
	if !bytes.Contains(streamRec.Body.Bytes(), []byte("chat.completion.chunk")) || !bytes.Contains(streamRec.Body.Bytes(), []byte("data: [DONE]")) {
		t.Fatalf("POST /v1/chat/completions(stream) body = %q, want chat chunk SSE and done marker", streamRec.Body.String())
	}
}

func TestAdminFailureBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
	})
	seedAdminToken(t, env, repository.RoleTenantAdmin, "tenant-admin-fail", "secret")
	seedAdminToken(t, env, repository.RoleSuperAdmin, "super-admin-fail", "secret")

	adminToken := "tenant-admin-fail:secret"
	superToken := "super-admin-fail:secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/users", adminToken, `{"name":"bad","role":"super_admin"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /admin/users(super_admin role) status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPost, "/admin/users", superToken, `{"name":"missing-tenant"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /admin/users(missing tenant_id) status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/users/missing", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /admin/users/missing status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPut, "/admin/users/missing", adminToken, `{"status":"inactive"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT /admin/users/missing status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodDelete, "/admin/users/missing", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE /admin/users/missing status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPost, "/admin/users/missing/reset", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /admin/users/missing/reset status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/users/missing/usage", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /admin/users/missing/usage status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/providers/missing", adminToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /admin/providers/missing status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/tenants/missing", superToken, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /admin/tenants/missing status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPut, "/admin/tenants/missing", superToken, `{"status":"inactive"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT /admin/tenants/missing status = %d, want %d: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPost, "/admin/tenants/tenant-a/providers", superToken, `{"providers":["missing-provider"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /admin/tenants/tenant-a/providers(bad provider) status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestAdminAPIKeysUsageAndRouteTraceEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-upstream",
			"object":  "chat.completion",
			"created": 1700000000,
			"model":   "provider-model",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "trace hello",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     2,
				"completion_tokens": 3,
				"total_tokens":      5,
			},
		})
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
	})
	seedAdminToken(t, env, repository.RoleTenantAdmin, "tenant-admin-key", "tenant-admin-secret")
	token := "tenant-admin-key:tenant-admin-secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/users", token, `{"name":"key-owner","email":"owner@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/users status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	userPayload := decodeBodyMap(t, rec)["data"].(map[string]any)
	userID := userPayload["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/keys", token, `{"user_id":"`+userID+`","budget_usd":6.5,"rate_limit_qps":7,"allowed_models":["provider-model"],"allowed_providers":["test-openai"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/keys status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	keyPayload := decodeBodyMap(t, rec)["data"].(map[string]any)
	keyID := keyPayload["id"].(string)
	if keyPayload["budget_usd"] != float64(6.5) || keyPayload["rate_limit_qps"] != float64(7) {
		t.Fatalf("POST /admin/keys payload = %#v, want key budget and qps", keyPayload)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/keys?user_id="+userID, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/keys status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/keys/"+keyID, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/keys/:id status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodPut, "/admin/keys/"+keyID, token, `{"status":"inactive","rate_limit_qps":9}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /admin/keys/:id status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	updatedKey := decodeBodyMap(t, rec)["data"].(map[string]any)
	if updatedKey["status"] != repository.StatusInactive || updatedKey["rate_limit_qps"] != float64(9) {
		t.Fatalf("PUT /admin/keys/:id payload = %#v, want inactive key with updated qps", updatedKey)
	}

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/keys/"+keyID+"/rotate", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/keys/:id/rotate status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rotatedKey := decodeBodyMap(t, rec)["data"].(map[string]any)
	if rotatedKey["api_key"] == "" || rotatedKey["api_secret"] == "" {
		t.Fatalf("POST /admin/keys/:id/rotate payload = %#v, want rotated credentials", rotatedKey)
	}

	rec = performJSONRequest(t, env, http.MethodPost, "/v1/responses", token, `{"model":"public-model","input":"hello trace"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/responses status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	responsePayload := decodeBodyMap(t, rec)
	responseID := responsePayload["id"].(string)
	if responsePayload["object"] != "response" {
		t.Fatalf("POST /v1/responses payload = %#v, want response object", responsePayload)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/responses/"+responseID+"/trace", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/responses/:id/trace status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	tracePayload := decodeBodyMap(t, rec)["data"].(map[string]any)
	trace := tracePayload["trace"].(map[string]any)
	if trace["final_provider"] != "test-openai" {
		t.Fatalf("GET /admin/responses/:id/trace payload = %#v, want final provider trace", trace)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/usage/summary?days=7", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/usage/summary status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	summaryPayload := decodeBodyMap(t, rec)["data"].(map[string]any)
	if summaryPayload["summary"].(map[string]any)["total_requests"] == nil {
		t.Fatalf("GET /admin/usage/summary payload = %#v, want summary body", summaryPayload)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/usage/breakdown?dimension=provider", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/usage/breakdown status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	breakdownPayload := decodeBodyMap(t, rec)["data"].(map[string]any)
	if len(breakdownPayload["rows"].([]any)) == 0 {
		t.Fatalf("GET /admin/usage/breakdown payload = %#v, want breakdown rows", breakdownPayload)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/usage/trend?period=day&limit=7", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/usage/trend status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/keys/"+keyID+"/revoke", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/keys/:id/revoke status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	revokedKey := decodeBodyMap(t, rec)["data"].(map[string]any)
	if revokedKey["status"] != repository.StatusRevoked {
		t.Fatalf("POST /admin/keys/:id/revoke payload = %#v, want revoked status", revokedKey)
	}

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/audit", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/audit status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	auditPayload := decodeBodyMap(t, rec)["data"].([]any)
	if len(auditPayload) == 0 {
		t.Fatal("GET /admin/audit data = empty, want audit entries")
	}
}

func TestAdminServiceLifecycleAndSubscriptionReviewEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chat-service-admin",
			"object":  "chat.completion",
			"created": 1,
			"model":   "provider-model",
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": "service admin hello"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
		})
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
	})
	seedAdminToken(t, env, repository.RoleTenantAdmin, "tenant-admin-service", "tenant-admin-secret")
	token := "tenant-admin-service:tenant-admin-secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/services", token, `{
		"name":"Greeting Service",
		"request_prefix":"greet-api",
		"default_provider":"test-openai",
		"default_model":"provider-model",
		"enabled":true,
		"config":{"surfaces":["invoke","responses"],"prompt_template":{"user_template":"Hello {{name}}","variables":[{"name":"name","required":true}]}}
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/services status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	servicePayload := decodeBodyMap(t, rec)["data"].(map[string]any)
	serviceID := servicePayload["service"].(map[string]any)["id"].(string)
	versionID := servicePayload["initial_version"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/services", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/services status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/services/"+serviceID, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/services/:id status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPost, "/admin/services/"+serviceID+"/publish", token, `{"version_id":"`+versionID+`","mode":"published"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/services/:id/publish status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPost, "/admin/services/"+serviceID+"/versions", token, ``)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/services/:id/versions status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	secondVersionID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)
	rec = performJSONRequest(t, env, http.MethodPost, "/admin/services/"+serviceID+"/publish", token, `{"version_id":"`+secondVersionID+`","mode":"staged"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/services/:id/publish(staged) status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPost, "/admin/services/"+serviceID+"/promote", token, ``)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/services/:id/promote status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPost, "/admin/services/"+serviceID+"/rollback", token, `{"version_id":"`+versionID+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/services/:id/rollback status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/services/"+serviceID+"/subscriptions", token, `{"consumer_name":"consumer-a","consumer_email":"consumer@example.com","requested_budget_usd":4.5,"requested_rate_limit_qps":6}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /admin/services/:id/subscriptions status = %d, want %d: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	subscriptionID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/services/"+serviceID+"/subscriptions", token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/services/:id/subscriptions status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodGet, "/admin/subscriptions/"+subscriptionID, token, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/subscriptions/:id status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec = performJSONRequest(t, env, http.MethodPost, "/admin/subscriptions/"+subscriptionID+"/review", token, `{"decision":"approve","review_note":"ok"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /admin/subscriptions/:id/review status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	reviewPayload := decodeBodyMap(t, rec)["data"].(map[string]any)
	apiKeyPayload := reviewPayload["api_key"].(map[string]any)
	if len(apiKeyPayload["allowed_services"].([]any)) != 1 {
		t.Fatalf("approved api key payload = %#v, want allowed_services scope", apiKeyPayload)
	}
}

func TestHandlerUtilityFunctions(t *testing.T) {
	if got, want := scopedTenant(nil), ""; got != want {
		t.Fatalf("scopedTenant(nil) = %q, want %q", got, want)
	}
	if got, want := providerNames(nil), []string(nil); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("providerNames(nil) = %v, want %v", got, want)
	}
	if providerStatus(nil) != "unknown" || providerLoad(nil) != 0 || remaining(&repository.UserRecord{Quota: -1}) != -1 || usagePercent(&repository.UserStats{}) != 0 || errorRate(0, 1) != 0 {
		t.Fatal("handler helper functions returned unexpected result")
	}
}
