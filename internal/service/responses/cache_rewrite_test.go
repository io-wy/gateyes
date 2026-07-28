package responses

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

func newPromptRewriteService() *Service {
	return New(&Dependencies{
		Config: &config.Config{
			Cache: config.CacheConfig{
				Enabled:       true,
				PromptRewrite: true,
			},
		},
		Cache: newMockCache(),
	})
}

func TestBuildCacheKeyNormalizesAnthropicCCH(t *testing.T) {
	svc := newPromptRewriteService()
	identity := &repository.AuthIdentity{TenantID: "t1"}

	req1 := &provider.ResponseRequest{
		Model:   "claude-sonnet",
		Surface: "messages",
		Input:   "hello",
		Options: &provider.RequestOptions{
			System: "x-anthropic-billing-header: cc_version=2.1; cch=5a235;\nYou are helpful.",
		},
	}
	req2 := &provider.ResponseRequest{
		Model:   "claude-sonnet",
		Surface: "messages",
		Input:   "hello",
		Options: &provider.RequestOptions{
			System: "x-anthropic-billing-header: cc_version=2.1; cch=abc99;\nYou are helpful.",
		},
	}

	if svc.buildCacheKey(context.Background(), identity, req1) != svc.buildCacheKey(context.Background(), identity, req2) {
		t.Fatal("cache key should ignore dynamic cch values")
	}

	req2.Options.System = "x-anthropic-billing-header: cc_version=2.1; cch=abc99;\nYou are terse."
	if svc.buildCacheKey(context.Background(), identity, req1) == svc.buildCacheKey(context.Background(), identity, req2) {
		t.Fatal("cache key should still differ when stable system instructions differ")
	}
}

func TestBuildCacheKeyNormalizesClaudeCodeDateMarker(t *testing.T) {
	svc := newPromptRewriteService()
	identity := &repository.AuthIdentity{TenantID: "t1"}

	req1 := &provider.ResponseRequest{
		Model:   "claude-sonnet",
		Surface: "messages",
		Input:   "hello",
		Options: &provider.RequestOptions{
			System: "# currentDate\nToday\u2019s date is 2026/07/24.\nFollow the instructions.",
		},
	}
	req2 := &provider.ResponseRequest{
		Model:   "claude-sonnet",
		Surface: "messages",
		Input:   "hello",
		Options: &provider.RequestOptions{
			System: "# currentDate\nToday's date is 2026-07-24.\nFollow the instructions.",
		},
	}

	if svc.buildCacheKey(context.Background(), identity, req1) != svc.buildCacheKey(context.Background(), identity, req2) {
		t.Fatal("cache key should normalize Claude Code date markers")
	}
}

func TestApplyCachePromptRewriteInjectsPromptCacheKey(t *testing.T) {
	svc := newPromptRewriteService()
	identity := &repository.AuthIdentity{
		TenantID:  "tenant-a",
		ProjectID: "project-a",
	}
	req := &provider.ResponseRequest{Model: "gpt-4.1", Surface: "responses", Input: "hello"}
	trace := &CacheTrace{}
	ctx := WithCacheTrace(context.Background(), trace)

	rewritten := svc.applyCachePromptRewrite(ctx, identity, req)
	if rewritten == req {
		t.Fatal("applyCachePromptRewrite should return a cloned request")
	}
	if rewritten.PromptCacheKey == "" {
		t.Fatal("prompt_cache_key should be injected")
	}
	if trace.PromptCacheKey != rewritten.PromptCacheKey {
		t.Fatalf("trace prompt cache key = %q, want %q", trace.PromptCacheKey, rewritten.PromptCacheKey)
	}
	if len(trace.Rewrites) == 0 || trace.Rewrites[0] != "prompt_cache_key" {
		t.Fatalf("trace rewrites = %v, want prompt_cache_key", trace.Rewrites)
	}
}

func TestApplyCachePromptRewriteKeepsClientPromptCacheKey(t *testing.T) {
	svc := newPromptRewriteService()
	identity := &repository.AuthIdentity{TenantID: "tenant-a", ProjectID: "project-a"}
	req := &provider.ResponseRequest{
		Model:          "gpt-4.1",
		Surface:        "responses",
		Input:          "hello",
		PromptCacheKey: "client-key",
	}

	rewritten := svc.applyCachePromptRewrite(context.Background(), identity, req)
	if rewritten.PromptCacheKey != "client-key" {
		t.Fatalf("prompt_cache_key = %q, want client-key", rewritten.PromptCacheKey)
	}
}

func TestPromptRewriteSavesUpstreamTokensOnDynamicCCH(t *testing.T) {
	baselineCalls, baselineTokens := runDynamicCCHPair(t, false)
	rewriteCalls, rewriteTokens := runDynamicCCHPair(t, true)

	if baselineCalls != 2 {
		t.Fatalf("baseline upstream calls = %d, want 2", baselineCalls)
	}
	if rewriteCalls != 1 {
		t.Fatalf("rewrite upstream calls = %d, want 1", rewriteCalls)
	}
	if baselineTokens <= rewriteTokens {
		t.Fatalf("rewrite should reduce upstream tokens, baseline=%d rewrite=%d", baselineTokens, rewriteTokens)
	}
	t.Logf("prompt rewrite token experiment: baseline=%d tokens, rewrite=%d tokens, saved=%d", baselineTokens, rewriteTokens, baselineTokens-rewriteTokens)
}

func runDynamicCCHPair(t *testing.T, rewrite bool) (int, int) {
	t.Helper()

	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chat-1","object":"chat.completion","created":1,"model":"provider-model","choices":[{"message":{"role":"assistant","content":"cached answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":2,"total_tokens":13}}`))
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})
	mc := newMockCache()
	env.service.cache = mc
	env.service.cfg.Cache = config.CacheConfig{
		Enabled:       true,
		DefaultTTL:    60,
		PromptRewrite: rewrite,
	}

	req1 := &provider.ResponseRequest{
		Model:   "public-model",
		Surface: "messages",
		Input:   "hello",
		Options: &provider.RequestOptions{
			System: "x-anthropic-billing-header: cc_version=2.1; cch=5a235;\nYou are helpful.",
		},
	}
	req2 := &provider.ResponseRequest{
		Model:   "public-model",
		Surface: "messages",
		Input:   "hello",
		Options: &provider.RequestOptions{
			System: "x-anthropic-billing-header: cc_version=2.1; cch=abc99;\nYou are helpful.",
		},
	}

	if _, err := env.service.Create(context.Background(), env.identity, req1, "session-a"); err != nil {
		t.Fatalf("first Create() error: %v", err)
	}
	waitForCacheWrite(t, mc, 1)
	if _, err := env.service.Create(context.Background(), env.identity, req2, "session-a"); err != nil {
		t.Fatalf("second Create() error: %v", err)
	}

	return int(calls.Load()), int(calls.Load()) * 13
}

func waitForCacheWrite(t *testing.T, mc *mockCache, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mc.mu.RLock()
		count := len(mc.data)
		mc.mu.RUnlock()
		if count >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d cache writes", want)
}
