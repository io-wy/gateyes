package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
)

type recordedMetric struct {
	surface    string
	provider   string
	result     string
	errorClass string
}

type fakeMetricsRecorder struct {
	entries []recordedMetric
}

func (f *fakeMetricsRecorder) RecordError(surface, providerName, result, errorClass string) {
	f.entries = append(f.entries, recordedMetric{
		surface:    surface,
		provider:   providerName,
		result:     result,
		errorClass: errorClass,
	})
}

func TestAuthMiddlewareRejectsInvalidSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mw := newTestMiddleware(t, repository.RoleTenantUser, -1, nil, nil)
	engine := gin.New()
	engine.POST("/test", mw.Auth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Authorization", "Bearer test-key:wrong-secret")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCorrelationMiddlewarePropagatesRequestContextAndHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	engine := gin.New()
	engine.Use(Correlation())
	engine.GET("/context", func(c *gin.Context) {
		requestCtx, ok := GetRequestContext(c)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "missing request context"})
			return
		}
		fromStdlib, ok := RequestContextFromContext(c.Request.Context())
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "missing stdlib request context"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"request_id":         requestCtx.RequestID,
			"trace_id":           requestCtx.TraceID,
			"traceparent":        requestCtx.Traceparent,
			"stdlib_request_id":  fromStdlib.RequestID,
			"stdlib_traceparent": fromStdlib.Traceparent,
		})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/context", nil)
	req.Header.Set(RequestIDHeader, "req-fixed")
	req.Header.Set(TraceparentHeader, "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["request_id"] != "req-fixed" || payload["stdlib_request_id"] != "req-fixed" {
		t.Fatalf("request context payload = %#v, want propagated request id", payload)
	}
	if payload["trace_id"] != "0123456789abcdef0123456789abcdef" || payload["stdlib_traceparent"] == "" {
		t.Fatalf("request context payload = %#v, want propagated trace info", payload)
	}
	if got := rec.Header().Get(RequestIDHeader); got != "req-fixed" {
		t.Fatalf("response X-Request-ID = %q, want req-fixed", got)
	}
	if got := rec.Header().Get(TraceparentHeader); got != "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01" {
		t.Fatalf("response traceparent = %q, want forwarded traceparent", got)
	}
}

func TestGuardLLMRequestRejectsDisallowedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mw := newTestMiddleware(t, repository.RoleTenantUser, -1, []string{"allowed-model"}, nil)
	engine := guardedEngine(mw)

	rec := httptest.NewRecorder()
	req := newGuardedRequest(t, provider.ResponseRequest{
		Model: "blocked-model",
		Input: "hello",
	})
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGuardLLMRequestRejectsQuotaExceeded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mw := newTestMiddleware(t, repository.RoleTenantUser, 2, nil, nil)
	engine := guardedEngine(mw)

	rec := httptest.NewRecorder()
	req := newGuardedRequest(t, provider.ResponseRequest{
		Model: "test-model",
		Input: "this prompt is deliberately long enough to exceed quota",
	})
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGuardLLMRequestRejectsRateLimitExceeded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	limiterCfg := &config.LimiterConfig{
		GlobalQPS:           100, // 确保全局默认 > 0
		GlobalTPM:           600000,
		GlobalTokenBurst:    10000,
		PerUserRequestBurst: 1,
		QueueSize:           8,
	}
	mw := newTestMiddleware(t, repository.RoleTenantUser, -1, nil, limiterCfg)
	engine := guardedEngine(mw)

	first := httptest.NewRecorder()
	firstReq := newGuardedRequest(t, provider.ResponseRequest{
		Model: "test-model",
		Input: "hello",
	})
	firstReq.Header.Set("Authorization", "Bearer test-key:test-secret")
	engine.ServeHTTP(first, firstReq)
	if first.Code != http.StatusOK {
		t.Fatalf("expected first request to pass, got %d: %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	secondReq := newGuardedRequest(t, provider.ResponseRequest{
		Model: "test-model",
		Input: "hello",
	})
	secondReq.Header.Set("Authorization", "Bearer test-key:test-secret")
	engine.ServeHTTP(second, secondReq)

	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", second.Code, second.Body.String())
	}
}

func TestGuardLLMRequestSetsMetaAndPreservesBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mw := newTestMiddleware(t, repository.RoleTenantUser, -1, nil, nil)
	engine := guardedEngine(mw)

	rec := httptest.NewRecorder()
	req := newGuardedRequest(t, provider.ResponseRequest{
		Model: "test-model",
		Input: []provider.Message{
			{Role: "user", Content: provider.TextBlocks("hello world")},
		},
	})
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Model           string `json:"model"`
		EstimatedTokens int    `json:"estimated_tokens"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Model != "test-model" {
		t.Fatalf("expected rebound body to preserve model, got %q", payload.Model)
	}
	if payload.EstimatedTokens <= 0 {
		t.Fatalf("expected estimated tokens > 0, got %d", payload.EstimatedTokens)
	}
}

func guardedEngine(mw *Middleware) *gin.Engine {
	engine := gin.New()
	engine.POST("/guarded", mw.Auth(), mw.GuardLLMRequest(), func(c *gin.Context) {
		var req provider.ResponseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		meta, _ := GetRequestMeta(c)
		c.JSON(http.StatusOK, gin.H{
			"model":            req.Model,
			"estimated_tokens": meta.EstimatedTokens,
		})
	})
	return engine
}

func newGuardedRequest(t *testing.T, payload provider.ResponseRequest) *http.Request {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return httptest.NewRequest(http.MethodPost, "/guarded", bytes.NewReader(body))
}

func newTestMiddleware(
	t *testing.T,
	role string,
	quota int,
	models []string,
	limiterCfg *config.LimiterConfig,
) *Middleware {
	t.Helper()

	ctx := context.Background()
	database, err := db.Open(config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "middleware.db"),
		AutoMigrate:            true,
		MaxOpenConns:           1,
		MaxIdleConns:           1,
		ConnMaxLifetimeSeconds: 60,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	store := sqlstore.New(database)
	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-a",
		Slug:   "tenant-a",
		Name:   "tenant-a",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenant.ID,
		Key:        "test-key",
		SecretHash: repository.HashSecret("test-secret"),
		Name:       "test-user",
		Email:      "test@example.com",
		Role:       role,
		Quota:      quota,
		QPS:        100,
		Models:     models,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	var limiterSvc *limiter.Limiter
	if limiterCfg != nil {
		limiterSvc = limiter.NewLimiter(*limiterCfg)
		t.Cleanup(limiterSvc.Stop)
	}

	return New(store, limiterSvc, nil, nil, nil)
}

func TestMiddlewareMetricsContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)

	metrics := &fakeMetricsRecorder{}
	mw := newTestMiddlewareWithMetrics(t, repository.RoleTenantUser, 2, []string{"allowed-model"}, &config.LimiterConfig{
		GlobalQPS:           100,
		GlobalTPM:           600000,
		GlobalTokenBurst:    10000,
		PerUserRequestBurst: 1,
		QueueSize:           8,
	}, metrics)
	engine := guardedEngine(mw)

	// invalid auth
	rec := httptest.NewRecorder()
	req := newGuardedRequest(t, provider.ResponseRequest{Model: "allowed-model", Input: "hello"})
	req.Header.Set("Authorization", "Bearer test-key:wrong-secret")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}

	// invalid request
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/guarded", strings.NewReader(`{invalid`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	// model not allowed
	rec = httptest.NewRecorder()
	req = newGuardedRequest(t, provider.ResponseRequest{Model: "blocked-model", Input: "hello"})
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}

	// quota exceeded
	rec = httptest.NewRecorder()
	req = newGuardedRequest(t, provider.ResponseRequest{Model: "allowed-model", Input: "this prompt is deliberately long enough to exceed quota"})
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(metrics.entries) != 4 {
		t.Fatalf("metrics entries = %d, want 4: %+v", len(metrics.entries), metrics.entries)
	}
	if metrics.entries[0].result != metricsResultAuthError || metrics.entries[0].errorClass != "invalid_api_key" {
		t.Fatalf("first metric = %+v, want auth invalid_api_key", metrics.entries[0])
	}
	if metrics.entries[1].result != metricsResultClientError || metrics.entries[1].errorClass != "invalid_request" {
		t.Fatalf("second metric = %+v, want client invalid_request", metrics.entries[1])
	}
	if metrics.entries[2].result != metricsResultAuthError || metrics.entries[2].errorClass != "model_not_allowed" {
		t.Fatalf("third metric = %+v, want auth model_not_allowed", metrics.entries[2])
	}
	if metrics.entries[3].result != metricsResultRateLimited || metrics.entries[3].errorClass != "quota_exceeded" {
		t.Fatalf("fourth metric = %+v, want rate_limited quota_exceeded", metrics.entries[3])
	}
}

func newTestMiddlewareWithMetrics(
	t *testing.T,
	role string,
	quota int,
	models []string,
	limiterCfg *config.LimiterConfig,
	metrics MetricsRecorder,
) *Middleware {
	t.Helper()

	ctx := context.Background()
	database, err := db.Open(config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "middleware-metrics.db"),
		AutoMigrate:            true,
		MaxOpenConns:           1,
		MaxIdleConns:           1,
		ConnMaxLifetimeSeconds: 60,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	store := sqlstore.New(database)
	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-a",
		Slug:   "tenant-a",
		Name:   "tenant-a",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenant.ID,
		Key:        "test-key",
		SecretHash: repository.HashSecret("test-secret"),
		Name:       "test-user",
		Email:      "test@example.com",
		Role:       role,
		Quota:      quota,
		QPS:        100,
		Models:     models,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	var limiterSvc *limiter.Limiter
	if limiterCfg != nil {
		limiterSvc = limiter.NewLimiter(*limiterCfg)
		t.Cleanup(limiterSvc.Stop)
	}

	return New(store, limiterSvc, nil, nil, metrics)
}
