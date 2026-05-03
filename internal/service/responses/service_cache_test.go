package responses

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/provider"
)

type mockCache struct {
	data map[string]*cache.Entry
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string]*cache.Entry)}
}

func (m *mockCache) Get(ctx context.Context, key string) (*cache.Entry, bool, error) {
	entry, ok := m.data[key]
	return entry, ok, nil
}

func (m *mockCache) Set(ctx context.Context, key string, e *cache.Entry, ttl time.Duration) error {
	m.data[key] = e
	return nil
}

func (m *mockCache) Delete(ctx context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockCache) Stats() cache.Stats { return cache.Stats{} }
func (m *mockCache) Close() error       { return nil }

type mockCacheMetrics struct {
	lookups []string
	writes  []string
}

func (m *mockCacheMetrics) RecordCacheLookup(layer, result string) {}
func (m *mockCacheMetrics) RecordCacheWrite(layer, result string)  {}
func (m *mockCacheMetrics) ObserveCacheValueSize(layer string, size int) {}
func (m *mockCacheMetrics) ObserveCacheGetDuration(layer string, d time.Duration) {}

func newCacheService(mc cache.Cache) *Service {
	return New(&Dependencies{
		Config: &config.Config{
			Cache: config.CacheConfig{Enabled: true, DefaultTTL: 60},
		},
		Cache:   mc,
		Metrics: &mockCacheMetrics{},
	})
}

func TestLookupCacheReturnsHitWhenEntryExists(t *testing.T) {
	mc := newMockCache()
	svc := newCacheService(mc)
	identity := &repository.AuthIdentity{TenantID: "t1"}
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}
	key := svc.buildCacheKey(identity, req)
	mc.data[key] = &cache.Entry{Response: []byte(`{"id":"cached"}`), Provider: "p1"}

	entry, hit := svc.lookupCache(context.Background(), identity, req)
	if !hit || entry == nil || entry.Provider != "p1" {
		t.Fatalf("lookupCache() = (%v, %v), want hit with provider p1", entry, hit)
	}
}

func TestLookupCacheReturnsMissWhenEntryMissing(t *testing.T) {
	mc := newMockCache()
	svc := newCacheService(mc)
	identity := &repository.AuthIdentity{TenantID: "t1"}
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}

	entry, hit := svc.lookupCache(context.Background(), identity, req)
	if hit || entry != nil {
		t.Fatalf("lookupCache() = (%v, %v), want miss", entry, hit)
	}
}

func TestLookupCacheSkipsWhenCacheDisabled(t *testing.T) {
	mc := newMockCache()
	svc := New(&Dependencies{
		Config:  &config.Config{Cache: config.CacheConfig{Enabled: false}},
		Cache:   mc,
		Metrics: &mockCacheMetrics{},
	})
	identity := &repository.AuthIdentity{TenantID: "t1"}
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}

	entry, hit := svc.lookupCache(context.Background(), identity, req)
	if hit || entry != nil {
		t.Fatalf("lookupCache() = (%v, %v), want skip when disabled", entry, hit)
	}
}

func TestLookupCacheSkipsStreamWhenConfigured(t *testing.T) {
	mc := newMockCache()
	svc := New(&Dependencies{
		Config: &config.Config{
			Cache: config.CacheConfig{Enabled: true, SkipStream: true},
		},
		Cache:   mc,
		Metrics: &mockCacheMetrics{},
	})
	identity := &repository.AuthIdentity{TenantID: "t1"}
	req := &provider.ResponseRequest{Model: "m1", Input: "hello", Stream: true}

	entry, hit := svc.lookupCache(context.Background(), identity, req)
	if hit || entry != nil {
		t.Fatalf("lookupCache() = (%v, %v), want skip for stream", entry, hit)
	}
}

func TestWriteCachePersistsEntry(t *testing.T) {
	mc := newMockCache()
	svc := newCacheService(mc)
	identity := &repository.AuthIdentity{TenantID: "t1"}
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}
	entry := &cache.Entry{Response: []byte(`{"id":"e1"}`), Provider: "p1"}

	svc.writeCache(context.Background(), identity, req, entry)
	time.Sleep(50 * time.Millisecond)

	key := svc.buildCacheKey(identity, req)
	got, ok := mc.data[key]
	if !ok || got.Provider != "p1" {
		t.Fatalf("writeCache() did not persist entry, got=%v", got)
	}
}

func TestWriteCacheSkipsNilEntry(t *testing.T) {
	mc := newMockCache()
	svc := newCacheService(mc)
	identity := &repository.AuthIdentity{TenantID: "t1"}
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}

	svc.writeCache(context.Background(), identity, req, nil)
	time.Sleep(50 * time.Millisecond)

	key := svc.buildCacheKey(identity, req)
	if _, ok := mc.data[key]; ok {
		t.Fatal("writeCache() should skip nil entry")
	}
}

func TestBuildCacheKeyIsDeterministic(t *testing.T) {
	svc := newCacheService(newMockCache())
	identity := &repository.AuthIdentity{TenantID: "t1"}
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}

	k1 := svc.buildCacheKey(identity, req)
	k2 := svc.buildCacheKey(identity, req)
	if k1 != k2 {
		t.Fatalf("buildCacheKey() not deterministic: %q vs %q", k1, k2)
	}
}

func TestBuildCacheKeyDiffersByTenant(t *testing.T) {
	svc := newCacheService(newMockCache())
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}

	k1 := svc.buildCacheKey(&repository.AuthIdentity{TenantID: "t1"}, req)
	k2 := svc.buildCacheKey(&repository.AuthIdentity{TenantID: "t2"}, req)
	if k1 == k2 {
		t.Fatal("buildCacheKey() should differ by tenant")
	}
}

func TestShouldSkipCacheReturnsTrueWhenDisabled(t *testing.T) {
	svc := New(&Dependencies{
		Config: &config.Config{Cache: config.CacheConfig{Enabled: false}},
	})
	req := &provider.ResponseRequest{Model: "m1"}
	if !svc.shouldSkipCache(req) {
		t.Fatal("shouldSkipCache() = false, want true when disabled")
	}
}

func TestShouldSkipCacheReturnsTrueForToolsWhenConfigured(t *testing.T) {
	svc := New(&Dependencies{
		Config: &config.Config{Cache: config.CacheConfig{Enabled: true, SkipTools: true}},
	})
	req := &provider.ResponseRequest{Model: "m1", Tools: []any{map[string]any{"type": "function"}}}
	if !svc.shouldSkipCache(req) {
		t.Fatal("shouldSkipCache() = false, want true for tools")
	}
}

func TestReplayCachedStreamEmitsEvents(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})
	entry := &cache.Entry{
		Response: []byte(`{"id":"orig","object":"response","created":1,"model":"m1","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"cached"}]}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`),
		Provider: "p1",
	}

	out := make(chan provider.ResponseEvent, 4)
	errCh := make(chan error, 1)
	env.service.replayCachedStream(context.Background(), env.identity, &provider.ResponseRequest{Model: "m1"}, entry, "resp-1", out, errCh)
	close(out)
	close(errCh)

	var events []string
	for e := range out {
		events = append(events, e.Type)
	}
	if len(events) != 2 || events[0] != provider.EventResponseStarted || events[1] != provider.EventResponseCompleted {
		t.Fatalf("unexpected events: %v", events)
	}
}

func TestReplayCachedStreamReturnsErrorOnBadJSON(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})
	entry := &cache.Entry{Response: []byte(`{bad`), Provider: "p1"}

	out := make(chan provider.ResponseEvent, 1)
	errCh := make(chan error, 1)
	env.service.replayCachedStream(context.Background(), env.identity, &provider.ResponseRequest{Model: "m1"}, entry, "resp-1", out, errCh)
	close(out)
	close(errCh)

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error for bad JSON")
		}
	default:
		t.Fatal("expected error on errCh")
	}
}

func TestCacheLayerReturnsL1StreamForStream(t *testing.T) {
	svc := newCacheService(newMockCache())
	if svc.cacheLayer(true) != cache.LayerL1Stream {
		t.Fatal("cacheLayer(true) != l1_stream")
	}
}

func TestCacheLayerReturnsL1ForNonStream(t *testing.T) {
	svc := newCacheService(newMockCache())
	if svc.cacheLayer(false) != cache.LayerL1 {
		t.Fatal("cacheLayer(false) != l1")
	}
}
