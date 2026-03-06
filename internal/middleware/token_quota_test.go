package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gateyes/internal/config"
	"gateyes/internal/requestmeta"
)

func TestMemoryBudgetServiceConsumeAndAdjust(t *testing.T) {
	service := NewMemoryBudgetService()

	first, err := service.Consume(t.Context(), []budgetCounter{{
		Key:   "k1",
		Limit: 10,
		Cost:  4,
		TTL:   time.Minute,
	}})
	if err != nil {
		t.Fatalf("consume failed: %v", err)
	}
	if !first.Allowed {
		t.Fatalf("expected allowed")
	}

	second, err := service.Consume(t.Context(), []budgetCounter{{
		Key:   "k1",
		Limit: 10,
		Cost:  7,
		TTL:   time.Minute,
	}})
	if err != nil {
		t.Fatalf("second consume failed: %v", err)
	}
	if second.Allowed {
		t.Fatalf("expected denied on limit exceed")
	}

	if err := service.Adjust(t.Context(), []budgetAdjustment{{
		Key:   "k1",
		Delta: -2,
		TTL:   time.Minute,
	}}); err != nil {
		t.Fatalf("adjust failed: %v", err)
	}

	third, err := service.Consume(t.Context(), []budgetCounter{{
		Key:   "k1",
		Limit: 10,
		Cost:  6,
		TTL:   time.Minute,
	}})
	if err != nil {
		t.Fatalf("third consume failed: %v", err)
	}
	if !third.Allowed {
		t.Fatalf("expected allowed after refund adjustment")
	}
}

func TestRateLimiterMultiDimensionUserModel(t *testing.T) {
	cfg := config.RateLimitConfig{
		Enabled: true,
		Backend: "memory",
		Rules: []config.RateLimitRuleConfig{
			{
				Name:              "rpm-user-model",
				Enabled:           true,
				Dimensions:        []string{"user", "model"},
				RequestsPerMinute: 1,
			},
		},
	}
	auth := config.AuthConfig{
		Header:     "Authorization",
		QueryParam: "api_key",
	}

	rl := NewRateLimiter(cfg, auth)
	handler := rl.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	makeReq := func(model string) *httptest.ResponseRecorder {
		body := []byte(`{"model":"` + model + `","messages":[{"role":"user","content":"hi"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer user-a")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := makeReq("gpt-4o-mini"); rec.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", rec.Code)
	}
	if rec := makeReq("gpt-4o-mini"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second same model expected 429, got %d", rec.Code)
	}
	if rec := makeReq("gpt-4.1"); rec.Code != http.StatusOK {
		t.Fatalf("different model expected 200, got %d", rec.Code)
	}
}

func TestQuotaTokenAdjustmentByUsage(t *testing.T) {
	cfg := config.QuotaConfig{
		Enabled:           true,
		Backend:           "memory",
		DefaultCompletion: 1,
		Rules: []config.QuotaRuleConfig{
			{
				Name:         "daily-user",
				Enabled:      true,
				Dimensions:   []string{"user"},
				TokensPerDay: 30,
			},
		},
	}
	auth := config.AuthConfig{
		Header:     "Authorization",
		QueryParam: "api_key",
	}

	quota := NewQuota(cfg, auth)
	handler := quota.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set(requestmeta.HeaderUsagePromptTokens, "10")
		r.Header.Set(requestmeta.HeaderUsageCompletionTokens, "5")
		r.Header.Set(requestmeta.HeaderUsageTotalTokens, "15")
		w.WriteHeader(http.StatusOK)
	}))

	makeReq := func() *httptest.ResponseRecorder {
		body := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello world"}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer user-a")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := makeReq(); rec.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", rec.Code)
	}
	if rec := makeReq(); rec.Code != http.StatusOK {
		t.Fatalf("second request expected 200, got %d", rec.Code)
	}
	if rec := makeReq(); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third request expected 429, got %d", rec.Code)
	}
}
