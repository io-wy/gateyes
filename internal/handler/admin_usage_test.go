package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/repository"
)

func TestGetBudgets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-budget", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodGet, "/admin/budgets", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET budgets status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"]
	if data == nil {
		t.Fatal("expected data in response")
	}
}

func TestGetUsageSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-usage", "secret").APIKey + ":" + "secret"
	identity := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-usage", "secret")

	ctx := context.Background()
	now := time.Now().UTC()
	_ = env.store.CreateUsageRecord(ctx, repository.UsageRecord{
		ID: "u-1", TenantID: identity.TenantID, UserID: identity.UserID,
		APIKeyID: identity.APIKeyID, ProviderName: "test-openai", Model: "m",
		PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2, LatencyMs: 10,
		Status: "success", CreatedAt: now,
	})

	rec := performJSONRequest(t, env, http.MethodGet, "/admin/usage/summary?days=7", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET usage summary status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].(map[string]any)
	if data["summary"] == nil {
		t.Fatal("expected summary in response")
	}
}

func TestGetUsageBreakdown(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-break", "secret").APIKey + ":" + "secret"
	identity := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-break", "secret")

	ctx := context.Background()
	now := time.Now().UTC()
	_ = env.store.CreateUsageRecord(ctx, repository.UsageRecord{
		ID: "u-2", TenantID: identity.TenantID, UserID: identity.UserID,
		APIKeyID: identity.APIKeyID, ProviderName: "test-openai", Model: "m",
		PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2, LatencyMs: 10,
		Status: "success", CreatedAt: now,
	})

	rec := performJSONRequest(t, env, http.MethodGet, "/admin/usage/breakdown?dimension=provider", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET usage breakdown status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].(map[string]any)
	if data["rows"] == nil {
		t.Fatal("expected rows in response")
	}
}

func TestGetUsageTrend(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-trend", "secret").APIKey + ":" + "secret"
	identity := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-trend", "secret")

	ctx := context.Background()
	now := time.Now().UTC()
	_ = env.store.CreateUsageRecord(ctx, repository.UsageRecord{
		ID: "u-3", TenantID: identity.TenantID, UserID: identity.UserID,
		APIKeyID: identity.APIKeyID, ProviderName: "test-openai", Model: "m",
		PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2, LatencyMs: 10,
		Status: "success", CreatedAt: now,
	})

	rec := performJSONRequest(t, env, http.MethodGet, "/admin/usage/trend?period=day&limit=7", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET usage trend status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].(map[string]any)
	if data["rows"] == nil {
		t.Fatal("expected rows in response")
	}
}

func TestResetUserUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-reset", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/users", adminToken, `{"name":"reset-user","email":"r@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", rec.Code, rec.Body.String())
	}
	userID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodPost, "/admin/users/"+userID+"/reset", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST reset usage status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].(map[string]any)
	if data["used"] != float64(0) {
		t.Fatalf("expected used=0, got %v", data["used"])
	}
}

func TestGetUserUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := seedAdminToken(t, env, repository.RoleTenantAdmin, "admin-uusage", "secret").APIKey + ":" + "secret"

	rec := performJSONRequest(t, env, http.MethodPost, "/admin/users", adminToken, `{"name":"usage-user","email":"u@example.com"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", rec.Code, rec.Body.String())
	}
	userID := decodeBodyMap(t, rec)["data"].(map[string]any)["id"].(string)

	rec = performJSONRequest(t, env, http.MethodGet, "/admin/users/"+userID+"/usage", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET user usage status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	data := decodeBodyMap(t, rec)["data"].(map[string]any)
	if data["user"] == nil {
		t.Fatal("expected user in response")
	}
}
