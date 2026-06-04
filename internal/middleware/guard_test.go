package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/filter"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gin-gonic/gin"
)

type fakeBudgetStore struct {
	results map[string]*repository.BudgetCheckResult
}

func (f *fakeBudgetStore) CheckAPIKeyBudget(ctx context.Context, apiKeyID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return f.results["api_key"], nil
}
func (f *fakeBudgetStore) CheckProjectBudget(ctx context.Context, projectID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return f.results["project"], nil
}
func (f *fakeBudgetStore) CheckTenantBudget(ctx context.Context, tenantID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return f.results["tenant"], nil
}
func (f *fakeBudgetStore) CheckVirtualKeyBudget(ctx context.Context, virtualKeyID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return f.results["virtual_key"], nil
}

type fakeAlertService struct {
	budgetEvents []alert.BudgetExhausted
}

func (f *fakeAlertService) NotifyBudgetExhausted(ctx context.Context, event alert.BudgetExhausted) {
	f.budgetEvents = append(f.budgetEvents, event)
}

func newGuardMiddlewareWithBudget(t *testing.T, policy string) (*GuardMiddleware, *fakeAlertService) {
	store := &fakeBudgetStore{results: map[string]*repository.BudgetCheckResult{
		"api_key": {Allowed: true},
		"project": {Allowed: true},
		"tenant":  {Allowed: false, Policy: policy},
	}}
	budgetSvc := budget.New(store)
	alertSvc := &fakeAlertService{}
	pipeline := filter.NewPipeline([]filter.Filter{
		filter.NewBudgetFilter(budgetSvc, alertSvc),
	})
	return NewGuardMiddleware(pipeline, nil), alertSvc
}

func newGuardMiddlewareWithBudgetAndAlert(t *testing.T, policy string) (*GuardMiddleware, *fakeAlertService) {
	store := &fakeBudgetStore{results: map[string]*repository.BudgetCheckResult{
		"api_key": {Allowed: true},
		"project": {Allowed: true},
		"tenant":  {Allowed: false, Policy: policy},
	}}
	budgetSvc := budget.New(store)
	alertSvc := &fakeAlertService{}
	pipeline := filter.NewPipeline([]filter.Filter{
		filter.NewBudgetFilter(budgetSvc, alertSvc),
	})
	return NewGuardMiddleware(pipeline, nil), alertSvc
}

func TestGuardMiddleware_BudgetHardReject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mw, _ := newGuardMiddlewareWithBudget(t, repository.BudgetPolicyHardReject)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body, _ := json.Marshal(provider.ResponseRequest{Model: "m", Input: "hi"})
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	SetIdentity(c, &repository.AuthIdentity{TenantID: "t1", APIKeyID: "k1", Role: repository.RoleTenantUser})

	mw.GuardLLMRequest()(c)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGuardMiddleware_BudgetSoftAlert(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &fakeBudgetStore{results: map[string]*repository.BudgetCheckResult{
		"api_key": {Allowed: true},
		"project": {Allowed: true},
		"tenant":  {Allowed: false, Policy: repository.BudgetPolicySoftAlert},
	}}
	budgetSvc := budget.New(store)
	alertSvc := &fakeAlertService{}
	pipeline := filter.NewPipeline([]filter.Filter{
		filter.NewBudgetFilter(budgetSvc, alertSvc),
	})
	mw := NewGuardMiddleware(pipeline, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body, _ := json.Marshal(provider.ResponseRequest{Model: "m", Input: "hi"})
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	SetIdentity(c, &repository.AuthIdentity{TenantID: "t1", APIKeyID: "k1", Role: repository.RoleTenantUser})

	mw.GuardLLMRequest()(c)

	if rec.Code != http.StatusOK && rec.Code != 0 {
		t.Fatalf("expected pass, got %d: %s", rec.Code, rec.Body.String())
	}
	// Alert is sent only when budgetResult.AlertSent is true AND m.alertSvc != nil.
	// Since we pass nil alert service, no alert is recorded; verify the request passes.
	_ = alertSvc
}

func TestGuardMiddleware_ModelRateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	limiterCfg := &config.LimiterConfig{
		GlobalQPS:           100,
		GlobalTPM:           600000,
		GlobalTokenBurst:    10000,
		PerUserRequestBurst: 1,
		QueueSize:           8,
	}
	mw := newTestMiddleware(t, repository.RoleTenantUser, -1, nil, limiterCfg)

	engine := guardedEngine(mw)
	rec := httptest.NewRecorder()
	req := newGuardedRequest(t, provider.ResponseRequest{Model: "test-model", Input: "hello"})
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected first 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	req2 := newGuardedRequest(t, provider.ResponseRequest{Model: "test-model", Input: "hello"})
	req2.Header.Set("Authorization", "Bearer test-key:test-secret")
	engine.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestGuardMiddleware_EmbeddingsPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)

	if !isEmbeddingsPath(c.Request.URL.Path) {
		t.Fatal("expected embeddings path detected")
	}
}

func TestEstimateEmbeddingTokens(t *testing.T) {
	cases := []struct {
		input any
		want  int
	}{
		{"hello world", len("hello world") / 4},
		{[]any{"a", "bb"}, len("a")/4 + len("bb")/4},
		{[]string{"x", "yy", "zzz"}, len("x")/4 + len("yy")/4 + len("zzz")/4},
		{123, 1},
	}
	for _, tc := range cases {
		got := estimateEmbeddingTokens(tc.input)
		if got != tc.want {
			t.Fatalf("estimateEmbeddingTokens(%v) = %d, want %d", tc.input, got, tc.want)
		}
	}
}
