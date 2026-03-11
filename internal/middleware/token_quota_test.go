package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateyes/internal/config"
	"gateyes/internal/requestmeta"
)

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
