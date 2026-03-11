package proxy

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gateyes/internal/config"
	"gateyes/internal/requestmeta"
)

func TestOpenAIProxyRetriesOnServerError(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"temporary"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	proxy := mustNewProxy(t, true, 1, map[string]config.ProviderConfig{
		"openai": {
			BaseURL: upstream.URL,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(
		`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`,
	))
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", got)
	}
}

func TestOpenAIProxyNoRetryWhenDisabled(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"failed"}`))
	}))
	defer upstream.Close()

	proxy := mustNewProxy(t, false, 3, map[string]config.ProviderConfig{
		"openai": {
			BaseURL: upstream.URL,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(
		`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`,
	))
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 upstream call, got %d", got)
	}
}

func TestOpenAIProxyVirtualKeyRestrictsProviderPool(t *testing.T) {
	var primaryCalls int32
	var backupCalls int32

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"provider":"primary"}`))
	}))
	defer primary.Close()

	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backupCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"provider":"backup"}`))
	}))
	defer backup.Close()

	cfg := config.GatewayConfig{
		OpenAIPathPrefix: "/v1",
		DefaultProvider:  "openai-primary",
		Routing: config.RoutingConfig{
			Enabled:  true,
			Strategy: "least-latency",
		},
	}
	authCfg := config.AuthConfig{
		VirtualKeys: map[string]config.VirtualKeyConfig{
			"vk-team-a": {
				Enabled:         true,
				Providers:       []string{"openai-backup"},
				DefaultProvider: "openai-backup",
			},
		},
	}

	proxy, err := NewOpenAIProxy(cfg, authCfg, map[string]config.ProviderConfig{
		"openai-primary": {BaseURL: primary.URL},
		"openai-backup":  {BaseURL: backup.URL},
	})
	if err != nil {
		t.Fatalf("NewOpenAIProxy failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(
		`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`,
	))
	req.Header.Set(requestmeta.HeaderVirtualKey, "vk-team-a")
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := atomic.LoadInt32(&backupCalls); got != 1 {
		t.Fatalf("expected backup provider calls=1, got %d", got)
	}
	if got := atomic.LoadInt32(&primaryCalls); got != 0 {
		t.Fatalf("expected primary provider calls=0, got %d", got)
	}
}

func TestOpenAIProxyRoundRobinStrategy(t *testing.T) {
	var aCalls int32
	var bCalls int32

	providerA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&aCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"provider":"a"}`))
	}))
	defer providerA.Close()

	providerB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&bCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"provider":"b"}`))
	}))
	defer providerB.Close()

	cfg := config.GatewayConfig{
		OpenAIPathPrefix: "/v1",
		DefaultProvider:  "openai-a",
		Routing: config.RoutingConfig{
			Enabled:  true,
			Strategy: "round-robin",
		},
	}

	proxy, err := NewOpenAIProxy(cfg, config.AuthConfig{}, map[string]config.ProviderConfig{
		"openai-a": {BaseURL: providerA.URL},
		"openai-b": {BaseURL: providerB.URL},
	})
	if err != nil {
		t.Fatalf("NewOpenAIProxy failed: %v", err)
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(
			`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`,
		))
		rec := httptest.NewRecorder()
		proxy.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d expected 200, got %d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}

	if atomic.LoadInt32(&aCalls) == 0 || atomic.LoadInt32(&bCalls) == 0 {
		t.Fatalf("expected both providers to receive traffic, got a=%d b=%d", aCalls, bCalls)
	}
}

func mustNewProxy(
	t *testing.T,
	retryEnabled bool,
	maxRetries int,
	providers map[string]config.ProviderConfig,
) *OpenAIProxy {
	t.Helper()

	cfg := config.GatewayConfig{
		OpenAIPathPrefix: "/v1",
		DefaultProvider:  "openai",
		Routing: config.RoutingConfig{
			Enabled:  true,
			Strategy: "least-latency",
			Retry: config.RetryConfig{
				Enabled:      retryEnabled,
				MaxRetries:   maxRetries,
				InitialDelay: config.Duration{Duration: 1 * time.Millisecond},
				MaxDelay:     config.Duration{Duration: 2 * time.Millisecond},
				Multiplier:   2,
			},
			HealthCheck: config.HealthCheckConfig{
				Enabled:  false,
				Interval: config.Duration{Duration: 1 * time.Second},
			},
		},
	}

	proxy, err := NewOpenAIProxy(cfg, config.AuthConfig{}, providers)
	if err != nil {
		t.Fatalf("NewOpenAIProxy failed: %v", err)
	}
	return proxy
}
