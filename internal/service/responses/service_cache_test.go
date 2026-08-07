package responses

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/provider"
)

type mockCache struct {
	mu   sync.RWMutex
	data map[string]*cache.Entry
}

func newMockCache() *mockCache {
	return &mockCache{data: make(map[string]*cache.Entry)}
}

func (m *mockCache) Get(ctx context.Context, key string) (*cache.Entry, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.data[key]
	return entry, ok, nil
}

func (m *mockCache) Set(ctx context.Context, key string, e *cache.Entry, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = e
	return nil
}

func (m *mockCache) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *mockCache) Stats() cache.Stats { return cache.Stats{} }
func (m *mockCache) Close() error       { return nil }

type mockCacheMetrics struct {
	lookups []string
	writes  []string
}

func (m *mockCacheMetrics) RecordCacheLookup(layer, result string)                {}
func (m *mockCacheMetrics) RecordCacheWrite(layer, result string)                 {}
func (m *mockCacheMetrics) ObserveCacheValueSize(layer string, size int)          {}
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
	key := svc.buildCacheKey(context.Background(), identity, req)
	_ = mc.Set(context.Background(), key, &cache.Entry{Response: []byte(`{"id":"cached"}`), Provider: "p1"}, 0)

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

	key := svc.buildCacheKey(context.Background(), identity, req)
	got, ok, _ := mc.Get(context.Background(), key)
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

	key := svc.buildCacheKey(context.Background(), identity, req)
	if _, ok, _ := mc.Get(context.Background(), key); ok {
		t.Fatal("writeCache() should skip nil entry")
	}
}

func TestSemanticCacheWriteThenHitAvoidsSecondUpstreamCall(t *testing.T) {
	var upstreamCalls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-upstream","object":"chat.completion","created":1700000000,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"semantic cached answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`))
	}))
	defer upstream.Close()

	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "embeddings") {
			t.Fatalf("unexpected embedding provider path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1,0,0]}],"model":"embed-model","usage":{"prompt_tokens":2,"total_tokens":2}}`))
	}))
	defer embeddings.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
		providerConfigs: []config.ProviderConfig{
			{Name: "test-openai", Type: "openai", BaseURL: upstream.URL, Endpoint: "chat", APIKey: "upstream-key", Model: "provider-model", Timeout: 5, Enabled: true, MaxTokens: 256},
			{Name: "semantic-embeddings", Type: "openai", BaseURL: embeddings.URL, Endpoint: "chat", APIKey: "embedding-key", Model: "embed-model", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	})
	enableSemanticCacheForTest(t, env, false)

	ctx := context.Background()
	first, err := env.service.Create(ctx, env.identity, &provider.ResponseRequest{
		Model:   "public-model",
		Surface: "responses",
		Input:   "explain cache hit rate",
	}, "session-semantic")
	if err != nil {
		t.Fatalf("first Create() error: %v", err)
	}
	if first.Response.OutputText() != "semantic cached answer" {
		t.Fatalf("first response text = %q", first.Response.OutputText())
	}

	second, err := env.service.Create(ctx, env.identity, &provider.ResponseRequest{
		Model:   "public-model",
		Surface: "responses",
		Input:   "describe cache-hit-ratio",
	}, "session-semantic-2")
	if err != nil {
		t.Fatalf("second Create() error: %v", err)
	}
	if second.Response.OutputText() != "semantic cached answer" {
		t.Fatalf("semantic hit response text = %q", second.Response.OutputText())
	}
	if got := atomic.LoadInt32(&upstreamCalls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 after semantic hit", got)
	}
}

func TestSemanticCacheRequiresOptInByDefault(t *testing.T) {
	var upstreamCalls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-upstream","object":"chat.completion","created":1700000000,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"upstream answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`))
	}))
	defer upstream.Close()

	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1,0,0]}],"model":"embed-model","usage":{"prompt_tokens":2,"total_tokens":2}}`))
	}))
	defer embeddings.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
		providerConfigs: []config.ProviderConfig{
			{Name: "test-openai", Type: "openai", BaseURL: upstream.URL, Endpoint: "chat", APIKey: "upstream-key", Model: "provider-model", Timeout: 5, Enabled: true, MaxTokens: 256},
			{Name: "semantic-embeddings", Type: "openai", BaseURL: embeddings.URL, Endpoint: "chat", APIKey: "embedding-key", Model: "embed-model", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	})
	enableSemanticCacheForTest(t, env, true)

	cached := provider.Response{
		ID:      "semantic-seeded",
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   "public-model",
		Status:  "completed",
		Output: []provider.ResponseOutput{{
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []provider.ResponseContent{{
				Type: "output_text",
				Text: "seeded semantic answer",
			}},
		}},
		Usage: provider.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3},
	}
	body, _ := json.Marshal(cached)
	usage, _ := json.Marshal(cached.Usage)
	if _, err := env.store.CreateSemanticCacheEntry(context.Background(), repository.CreateSemanticCacheParams{
		TenantID:            env.identity.TenantID,
		Surface:             "responses",
		Model:               "public-model",
		EmbeddingModel:      "embed-model",
		PromptHash:          "seeded",
		PromptCanonical:     []byte(`{"prompt":"seeded"}`),
		PromptText:          "seeded",
		Embedding:           []float64{1, 0, 0},
		ResponseBody:        body,
		ProviderName:        "test-openai",
		UsageBody:           usage,
		SimilarityThreshold: 0.92,
		ExpiresAt:           time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed semantic cache: %v", err)
	}

	result, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model:   "public-model",
		Surface: "responses",
		Input:   "same semantic prompt",
	}, "session-no-opt-in")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if result.Response.OutputText() != "upstream answer" {
		t.Fatalf("response text = %q, want upstream answer", result.Response.OutputText())
	}
	if got := atomic.LoadInt32(&upstreamCalls); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 when semantic cache is not opted in", got)
	}
}

func TestSemanticCacheTraceFieldsAreRecorded(t *testing.T) {
	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1,0,0]}],"model":"embed-model","usage":{"prompt_tokens":2,"total_tokens":2}}`))
	}))
	defer embeddings.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: "http://127.0.0.1:1",
		endpoint:    "chat",
		providers:   []string{"test-openai"},
		providerConfigs: []config.ProviderConfig{
			{Name: "test-openai", Type: "openai", BaseURL: "http://127.0.0.1:1", Endpoint: "chat", APIKey: "upstream-key", Model: "provider-model", Timeout: 5, Enabled: true, MaxTokens: 256},
			{Name: "semantic-embeddings", Type: "openai", BaseURL: embeddings.URL, Endpoint: "chat", APIKey: "embedding-key", Model: "embed-model", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	})
	enableSemanticCacheForTest(t, env, false)
	seedSemanticCacheEntry(t, env, "trace entry")
	trace := &CacheTrace{}
	ctx := WithCacheTrace(context.Background(), trace)

	result, err := env.service.Create(ctx, env.identity, &provider.ResponseRequest{
		Model:   "public-model",
		Surface: "responses",
		Input:   "trace me differently",
	}, "session-trace")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if result.Response.OutputText() != "trace entry" {
		t.Fatalf("semantic hit response text = %q", result.Response.OutputText())
	}
	if trace.EntryID == "" || trace.EmbeddingModel != "embed-model" || trace.Threshold == 0 {
		t.Fatalf("trace = %+v, want semantic fields recorded", trace)
	}
}

func TestSemanticCacheStreamHitReplaysTranscript(t *testing.T) {
	var upstreamCalls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"upstream\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"upstream\",\"created_at\":1,\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"upstream\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	embeddings := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[1,0,0]}],"model":"embed-model","usage":{"prompt_tokens":2,"total_tokens":2}}`))
	}))
	defer embeddings.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "responses",
		providers:   []string{"test-openai"},
		providerConfigs: []config.ProviderConfig{
			{Name: "test-openai", Type: "openai", BaseURL: upstream.URL, Endpoint: "responses", APIKey: "upstream-key", Model: "provider-model", Timeout: 5, Enabled: true, MaxTokens: 256},
			{Name: "semantic-embeddings", Type: "openai", BaseURL: embeddings.URL, Endpoint: "chat", APIKey: "embedding-key", Model: "embed-model", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	})
	enableSemanticCacheForTest(t, env, false)

	cached := provider.NewTextResponse("semantic-seeded", "public-model", "semantic stream", provider.Usage{PromptTokens: 2, CompletionTokens: 2, TotalTokens: 4})
	body, _ := json.Marshal(cached)
	usage, _ := json.Marshal(cached.Usage)
	if _, err := env.store.CreateSemanticCacheEntry(context.Background(), repository.CreateSemanticCacheParams{
		TenantID:        env.identity.TenantID,
		Surface:         "responses",
		Model:           "public-model",
		EmbeddingModel:  "embed-model",
		PromptHash:      "seeded-stream",
		PromptCanonical: []byte(`{"prompt":"seeded stream"}`),
		PromptText:      "seeded stream",
		Embedding:       []float64{1, 0, 0},
		ResponseBody:    body,
		StreamBody: semanticStreamBody([]cache.StreamEvent{
			{Type: provider.EventContentDelta, Delta: "semantic ", TextDelta: "semantic "},
			{Type: provider.EventContentDelta, Delta: "stream", TextDelta: "stream"},
			{Type: provider.EventResponseCompleted, Response: body},
		}),
		ProviderName:        "test-openai",
		UsageBody:           usage,
		SimilarityThreshold: 0.92,
		ExpiresAt:           time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed semantic stream cache: %v", err)
	}

	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model:   "public-model",
		Surface: "responses",
		Input:   "semantically same stream prompt",
		Stream:  true,
	}, "session-semantic-stream")
	if err != nil {
		t.Fatalf("CreateStream() error: %v", err)
	}

	var types []string
	var text string
	for event := range stream.Events {
		types = append(types, event.Type)
		if event.Type == provider.EventContentDelta {
			text += event.Text()
		}
	}
	for err := range stream.Errors {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
	}
	if got, want := strings.Join(types, ","), "response_started,content_delta,content_delta,response_completed"; got != want {
		t.Fatalf("event types = %s, want %s", got, want)
	}
	if text != "semantic stream" {
		t.Fatalf("stream text = %q, want semantic stream", text)
	}
	if got := atomic.LoadInt32(&upstreamCalls); got != 0 {
		t.Fatalf("upstream calls = %d, want 0 on semantic stream hit", got)
	}
}

func seedSemanticCacheEntry(t *testing.T, env *responsesTestEnv, text string) {
	t.Helper()
	cached := provider.Response{
		ID:      "semantic-seeded",
		Object:  "response",
		Created: time.Now().Unix(),
		Model:   "public-model",
		Status:  "completed",
		Output: []provider.ResponseOutput{{
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []provider.ResponseContent{{
				Type: "output_text",
				Text: text,
			}},
		}},
		Usage: provider.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3},
	}
	body, _ := json.Marshal(cached)
	usage, _ := json.Marshal(cached.Usage)
	if _, err := env.store.CreateSemanticCacheEntry(context.Background(), repository.CreateSemanticCacheParams{
		TenantID:            env.identity.TenantID,
		Surface:             "responses",
		Model:               "public-model",
		EmbeddingModel:      "embed-model",
		PromptHash:          "seeded",
		PromptCanonical:     []byte(`{"prompt":"seeded"}`),
		PromptText:          "seeded",
		Embedding:           []float64{1, 0, 0},
		ResponseBody:        body,
		ProviderName:        "test-openai",
		UsageBody:           usage,
		SimilarityThreshold: 0.92,
		ExpiresAt:           time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed semantic cache: %v", err)
	}
}

func enableSemanticCacheForTest(t *testing.T, env *responsesTestEnv, requireOptIn bool) {
	t.Helper()
	env.service.cache = newMockCache()
	env.service.cfg.Cache = config.CacheConfig{
		Enabled:    true,
		DefaultTTL: 60,
		Semantic: config.SemanticCacheConfig{
			Enabled:             true,
			Backend:             "pgvector",
			EmbeddingProvider:   "semantic-embeddings",
			EmbeddingModel:      "embed-model",
			Threshold:           0.92,
			MaxCandidates:       5,
			TTLSeconds:          60,
			WriteAsync:          false,
			AllowStream:         true,
			AllowedSurfaces:     []string{"responses"},
			RequireServiceOptIn: requireOptIn,
		},
	}
	embedder, ok := env.providerMgr.Get("semantic-embeddings")
	if !ok {
		t.Fatal("semantic embedding provider not found")
	}
	env.service.embedding = embedder
}

func TestBuildCacheKeyIsDeterministic(t *testing.T) {
	svc := newCacheService(newMockCache())
	identity := &repository.AuthIdentity{TenantID: "t1"}
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}

	k1 := svc.buildCacheKey(context.Background(), identity, req)
	k2 := svc.buildCacheKey(context.Background(), identity, req)
	if k1 != k2 {
		t.Fatalf("buildCacheKey() not deterministic: %q vs %q", k1, k2)
	}
}

func TestBuildCacheKeyDiffersByTenant(t *testing.T) {
	svc := newCacheService(newMockCache())
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}

	k1 := svc.buildCacheKey(context.Background(), &repository.AuthIdentity{TenantID: "t1"}, req)
	k2 := svc.buildCacheKey(context.Background(), &repository.AuthIdentity{TenantID: "t2"}, req)
	if k1 == k2 {
		t.Fatal("buildCacheKey() should differ by tenant")
	}
}

func TestShouldSkipCacheReturnsTrueWhenDisabled(t *testing.T) {
	svc := New(&Dependencies{
		Config: &config.Config{Cache: config.CacheConfig{Enabled: false}},
	})
	req := &provider.ResponseRequest{Model: "m1"}
	if !svc.shouldSkipCache(context.Background(), req) {
		t.Fatal("shouldSkipCache() = false, want true when disabled")
	}
}

func TestShouldSkipCacheReturnsTrueForToolsWhenConfigured(t *testing.T) {
	svc := New(&Dependencies{
		Config: &config.Config{Cache: config.CacheConfig{Enabled: true, SkipTools: true}},
	})
	req := &provider.ResponseRequest{Model: "m1", Tools: []any{map[string]any{"type": "function"}}}
	if !svc.shouldSkipCache(context.Background(), req) {
		t.Fatal("shouldSkipCache() = false, want true for tools")
	}
}

func TestReplayCachedStreamEmitsEvents(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
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

func TestReplayCachedStreamTranscriptEmitsDeltas(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})
	respBody := []byte(`{"id":"orig","object":"response","created":1,"model":"m1","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
	startBody := []byte(`{"id":"orig-start","object":"response","created":1,"model":"m1","status":"in_progress"}`)
	entry := &cache.Entry{
		Response: respBody,
		Provider: "p1",
		StreamTranscript: []cache.StreamEvent{
			{Type: provider.EventResponseStarted, Response: startBody},
			{Type: provider.EventContentDelta, Delta: "hel", TextDelta: "hel"},
			{Type: provider.EventContentDelta, Delta: "lo", TextDelta: "lo"},
			{Type: provider.EventResponseCompleted, Response: respBody},
		},
	}

	out := make(chan provider.ResponseEvent, 8)
	errCh := make(chan error, 1)
	env.service.replayCachedStream(context.Background(), env.identity, &provider.ResponseRequest{Model: "m1"}, entry, "resp-1", out, errCh)
	close(out)
	close(errCh)

	var types []string
	var text, completedID string
	for e := range out {
		types = append(types, e.Type)
		if e.Type == provider.EventContentDelta {
			text += e.Text()
		}
		if e.Type == provider.EventResponseCompleted && e.Response != nil {
			completedID = e.Response.ID
		}
	}
	if got, want := strings.Join(types, ","), "response_started,content_delta,content_delta,response_completed"; got != want {
		t.Fatalf("event types = %s, want %s", got, want)
	}
	if text != "hello" {
		t.Fatalf("stream text = %q, want hello", text)
	}
	if completedID != "resp-1" {
		t.Fatalf("completed response id = %q, want resp-1", completedID)
	}
}

func TestReplayCachedStreamReturnsErrorOnBadJSON(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
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
