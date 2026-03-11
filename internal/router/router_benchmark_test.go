package router_test

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateyes/internal/bootstrap"
	"gateyes/internal/config"
)

var benchRequestBody = []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`)
var benchResponseBody = []byte(`{"id":"chatcmpl-bench","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`)

func BenchmarkGatewayChatMinimal(b *testing.B) {
	engine, cleanup := newBenchEngine(b, benchOptions{
		enableMetrics: false,
		enableAuth:    false,
	})
	defer cleanup()

	benchmarkChat(b, engine, "")
}

func BenchmarkGatewayChatAuthVirtualKey(b *testing.B) {
	engine, cleanup := newBenchEngine(b, benchOptions{
		enableMetrics: false,
		enableAuth:    true,
	})
	defer cleanup()

	benchmarkChat(b, engine, "vk-bench")
}

func BenchmarkGatewayChatAuthVirtualKeyWithMetrics(b *testing.B) {
	engine, cleanup := newBenchEngine(b, benchOptions{
		enableMetrics: true,
		enableAuth:    true,
	})
	defer cleanup()

	benchmarkChat(b, engine, "vk-bench")
}

type benchOptions struct {
	enableMetrics bool
	enableAuth    bool
}

func newBenchEngine(b *testing.B, opts benchOptions) (*http.ServeMux, func()) {
	b.Helper()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(benchResponseBody)
	}))

	cfg := config.DefaultConfig()
	cfg.Metrics.Enabled = opts.enableMetrics
	cfg.Cache.Enabled = false
	cfg.RateLimit.Enabled = false
	cfg.Quota.Enabled = false
	cfg.Auth.Enabled = opts.enableAuth
	cfg.Auth.Keys = nil
	cfg.Auth.VirtualKeys = nil
	if opts.enableAuth {
		cfg.Auth.VirtualKeys = map[string]config.VirtualKeyConfig{
			"vk-bench": {
				Enabled:         true,
				Providers:       []string{"openai"},
				DefaultProvider: "openai",
			},
		}
	}

	cfg.Gateway.OpenAIPathPrefix = "/v1"
	cfg.Gateway.ProviderHeader = "X-Gateyes-Provider"
	cfg.Gateway.ProviderQuery = "provider"
	cfg.Gateway.DefaultProvider = "openai"
	cfg.Gateway.Routing.Enabled = true
	cfg.Gateway.Routing.Strategy = "least-latency"
	cfg.Gateway.Routing.Retry.Enabled = false
	cfg.Gateway.Routing.Fallback = nil

	cfg.Providers = map[string]config.ProviderConfig{
		"openai": {
			BaseURL:    upstream.URL,
			APIKey:     "sk-bench-upstream",
			AuthHeader: "Authorization",
			AuthScheme: "Bearer",
		},
	}

	engine, err := bootstrap.NewEngine(&cfg, nil)
	if err != nil {
		upstream.Close()
		b.Fatalf("new engine failed: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", engine)
	return mux, upstream.Close
}

func benchmarkChat(b *testing.B, handler http.Handler, token string) {
	b.Helper()
	b.ReportAllocs()
	b.SetBytes(int64(len(benchRequestBody)))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(benchRequestBody))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected status: %d", rec.Code)
		}
	}
}
