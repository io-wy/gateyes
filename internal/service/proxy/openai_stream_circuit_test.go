package proxy

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gateyes/internal/config"
	"gateyes/internal/requestmeta"
)

func TestOpenAIProxyCircuitBreakerThresholdFromConfig(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer upstream.Close()

	cfg := config.GatewayConfig{
		OpenAIPathPrefix: "/v1",
		DefaultProvider:  "openai",
		Routing: config.RoutingConfig{
			Enabled:  true,
			Strategy: "least-latency",
			Retry: config.RetryConfig{
				Enabled: false,
			},
			CircuitBreaker: config.CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 2,
				SuccessThreshold: 1,
				Timeout:          config.Duration{Duration: 5 * time.Minute},
				HalfOpenRequests: 1,
			},
		},
	}

	proxy, err := NewOpenAIProxy(cfg, config.AuthConfig{}, map[string]config.ProviderConfig{
		"openai": {BaseURL: upstream.URL},
	})
	if err != nil {
		t.Fatalf("NewOpenAIProxy failed: %v", err)
	}

	makeReq := func() {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(
			`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`,
		))
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
		}
	}

	makeReq()
	if got := proxy.isProviderTemporarilyUnhealthy("openai"); got {
		t.Fatalf("provider should still be closed after first failure")
	}

	makeReq()
	if got := proxy.isProviderTemporarilyUnhealthy("openai"); !got {
		t.Fatalf("provider should be open after second failure")
	}
}

func TestOpenAIProxyStreamingFallbackAndUsageHeaders(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"temporary"}`))
	}))
	defer primary.Close()

	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "data: {\"id\":\"x\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"model\":\"gpt-4o-mini\",\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer backup.Close()

	cfg := config.GatewayConfig{
		OpenAIPathPrefix: "/v1",
		ProviderHeader:   "X-Gateyes-Provider",
		DefaultProvider:  "openai-primary",
		Routing: config.RoutingConfig{
			Enabled:  true,
			Strategy: "least-latency",
			Fallback: []string{"openai-backup"},
			Retry: config.RetryConfig{
				Enabled:    true,
				MaxRetries: 0,
			},
			CircuitBreaker: config.CircuitBreakerConfig{
				Enabled:          true,
				FailureThreshold: 3,
				SuccessThreshold: 1,
				Timeout:          config.Duration{Duration: time.Minute},
				HalfOpenRequests: 1,
			},
		},
	}

	proxy, err := NewOpenAIProxy(cfg, config.AuthConfig{}, map[string]config.ProviderConfig{
		"openai-primary": {BaseURL: primary.URL},
		"openai-backup":  {BaseURL: backup.URL},
	})
	if err != nil {
		t.Fatalf("NewOpenAIProxy failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(
		`{"model":"gpt-4o-mini","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Gateyes-Provider", "openai-primary")
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := req.Header.Get(requestmeta.HeaderRetryCount); got != "0" {
		t.Fatalf("expected retry_count=0, got %q", got)
	}
	if got := req.Header.Get(requestmeta.HeaderFallbackCount); got != "1" {
		t.Fatalf("expected fallback_count=1, got %q", got)
	}
	if got := req.Header.Get(requestmeta.HeaderResolvedProvider); got != "openai-backup" {
		t.Fatalf("expected resolved provider openai-backup, got %q", got)
	}
	if got := req.Header.Get(requestmeta.HeaderUsageTotalTokens); got != "15" {
		t.Fatalf("expected usage total tokens 15, got %q", got)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("[DONE]")) {
		t.Fatalf("expected stream body to contain [DONE], got %s", rec.Body.String())
	}
}
