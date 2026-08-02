package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/service/provider"
)

// mockProvider 用于测试的 mock
type mockProvider struct {
	name   string
	model  string
	cost   float64
	load   int64
	labels map[string]string
}

func (m *mockProvider) Name() string    { return m.name }
func (m *mockProvider) Type() string    { return "mock" }
func (m *mockProvider) BaseURL() string { return "http://test.com" }
func (m *mockProvider) Model() string   { return m.model }
func (m *mockProvider) Labels() map[string]string {
	return m.labels
}
func (m *mockProvider) Weight() int       { return 0 }
func (m *mockProvider) UnitCost() float64 { return m.cost }
func (m *mockProvider) Cost(prompt, completion int) float64 {
	return float64(prompt+completion) * m.cost
}
func (m *mockProvider) CreateResponse(ctx context.Context, req *provider.ResponseRequest) (*provider.Response, error) {
	return nil, nil
}
func (m *mockProvider) StreamResponse(ctx context.Context, req *provider.ResponseRequest) (<-chan provider.ResponseEvent, <-chan error) {
	return nil, nil
}
func (m *mockProvider) CreateEmbedding(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, nil
}
func (m *mockProvider) CreateImageGeneration(ctx context.Context, req *provider.ImageGenerationRequest) (*provider.ImageGenerationResponse, error) {
	return nil, nil
}

func TestRouter_RoundRobin(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
	}
	r := NewRouter(cfg, nil)

	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 1.0},
		&mockProvider{name: "p3", model: "m3", cost: 1.0},
	}
	r.SetProviders(providers)

	// 轮询选择
	results := make(map[string]int)
	for i := 0; i < 6; i++ {
		p := r.Select("model1", "")
		results[p.Name()]++
	}

	// 每个应该被选中 2 次
	for _, p := range providers {
		if results[p.Name()] != 2 {
			t.Errorf("expected 2 selections for %s, got %d", p.Name(), results[p.Name()])
		}
	}
}

func TestRouter_Random(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "random",
	}
	r := NewRouter(cfg, nil)

	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 1.0},
		&mockProvider{name: "p3", model: "m3", cost: 1.0},
	}
	r.SetProviders(providers)

	// 随机选择，应该有变化
	results := make(map[string]int)
	for i := 0; i < 100; i++ {
		p := r.Select("model1", "")
		results[p.Name()]++
	}

	// 验证所有 provider 都被选中了
	if len(results) != 3 {
		t.Errorf("expected all providers to be selected, got %d", len(results))
	}
}

func TestRouter_LeastLoad(t *testing.T) {
	stats := provider.NewStats()
	p1 := &mockProvider{name: "p1", model: "m1", cost: 1.0}
	p2 := &mockProvider{name: "p2", model: "m2", cost: 1.0}
	p3 := &mockProvider{name: "p3", model: "m3", cost: 1.0}
	stats.Register(p1)
	stats.Register(p2)
	stats.Register(p3)

	cfg := config.RouterConfig{
		Strategy: "least_load",
	}
	r := NewRouter(cfg, stats)
	providers := []provider.Provider{p1, p2, p3}
	r.SetProviders(providers)

	// 初始负载都是 0，应该选择第一个
	p := r.Select("model1", "")
	if p.Name() != "p1" {
		t.Errorf("expected p1 first, got %s", p.Name())
	}

	// 增加 p1 负载
	stats.IncrementLoad("p1")
	stats.IncrementLoad("p1")

	// 现在应该选择 p2 或 p3
	p = r.Select("model1", "")
	if p.Name() == "p1" {
		t.Error("p1 should not be selected with higher load")
	}
}

func TestRouter_LeastTPM(t *testing.T) {
	stats := provider.NewStats()
	p1 := &mockProvider{name: "p1", model: "m1", cost: 1.0}
	p2 := &mockProvider{name: "p2", model: "m2", cost: 1.0}
	stats.Register(p1)
	stats.Register(p2)

	// p1 产生大量 token，p2 产生少量 token
	stats.RecordRequest("p1", true, 1000, 100)
	stats.RecordRequest("p2", true, 100, 100)

	cfg := config.RouterConfig{
		Strategy: "least_tpm",
	}
	r := NewRouter(cfg, stats)
	r.SetProviders([]provider.Provider{p1, p2})

	ordered := r.OrderCandidates(r.List(), RouteContext{})
	if len(ordered) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(ordered))
	}
	if ordered[0].Name() != "p2" {
		t.Errorf("expected p2 first (lower TPM), got %s", ordered[0].Name())
	}
	if ordered[1].Name() != "p1" {
		t.Errorf("expected p1 second (higher TPM), got %s", ordered[1].Name())
	}
}

func TestRouter_LeastLatency(t *testing.T) {
	stats := provider.NewStats()
	p1 := &mockProvider{name: "slow", model: "m1", cost: 1.0}
	p2 := &mockProvider{name: "fast", model: "m1", cost: 1.0}
	stats.Register(p1)
	stats.Register(p2)
	stats.RecordRequest("slow", true, 100, 300)
	stats.RecordRequest("fast", true, 100, 80)

	r := NewRouter(config.RouterConfig{Strategy: "least_latency"}, stats)
	r.SetProviders([]provider.Provider{p1, p2})

	ordered := r.OrderCandidates(r.List(), RouteContext{})
	if ordered[0].Name() != "fast" {
		t.Fatalf("least_latency selected %s, want fast", ordered[0].Name())
	}

	ordered, trace := r.ExplainOrderCandidates(r.List(), RouteContext{})
	if ordered[0].Name() != "fast" {
		t.Fatalf("ExplainOrderCandidates least_latency selected %s, want fast", ordered[0].Name())
	}
	if len(trace.Scores) != 2 || trace.Scores[0].Provider != "fast" || trace.Scores[0].Components["avg_latency_ms"] != 80 {
		t.Fatalf("trace.Scores = %#v, want fast avg_latency_ms=80 first", trace.Scores)
	}
}

func TestRouter_InferenceCacheStrategies(t *testing.T) {
	low := newMetricsServer(t, `vllm:gpu_cache_usage_perc 0.20
vllm:cpu_cache_usage_perc 0.10
`)
	high := newMetricsServer(t, `vllm:gpu_cache_usage_perc 0.80
vllm:cpu_cache_usage_perc 0.05
`)
	scraper := NewInferenceScraper(map[string]string{
		"low-cache":  low.URL,
		"high-cache": high.URL,
	}, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scraper.Start(ctx)
	waitForInferenceState(t, scraper, "low-cache")
	waitForInferenceState(t, scraper, "high-cache")

	p1 := &mockProvider{name: "high-cache", model: "m1", cost: 1.0}
	p2 := &mockProvider{name: "low-cache", model: "m1", cost: 1.0}
	providers := []provider.Provider{p1, p2}

	r := NewRouter(config.RouterConfig{Strategy: "least_gpu_cache"}, nil)
	r.SetInferenceScraper(scraper)
	ordered := r.OrderCandidates(providers, RouteContext{})
	if ordered[0].Name() != "low-cache" {
		t.Fatalf("least_gpu_cache selected %s, want low-cache", ordered[0].Name())
	}

	r = NewRouter(config.RouterConfig{Strategy: "least_kv_cache"}, nil)
	r.SetInferenceScraper(scraper)
	ordered = r.OrderCandidates(providers, RouteContext{})
	if ordered[0].Name() != "low-cache" {
		t.Fatalf("least_kv_cache selected %s, want low-cache", ordered[0].Name())
	}
}

func TestRouter_CostBased(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "cost_based",
	}
	r := NewRouter(cfg, nil)

	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0}, // 最贵
		&mockProvider{name: "p2", model: "m2", cost: 0.5}, // 中等
		&mockProvider{name: "p3", model: "m3", cost: 0.1}, // 最便宜
	}
	r.SetProviders(providers)

	// 应该总是选择最便宜的
	p := r.Select("model1", "")
	if p.Name() != "p3" {
		t.Errorf("expected p3 (cheapest), got %s", p.Name())
	}
}

func TestNormalizeRoutingProfile(t *testing.T) {
	tests := []struct {
		raw          string
		wantProfile  string
		wantStrategy string
	}{
		{"", "", ""},
		{"default", "", ""},
		{"latency", RoutingProfileLatency, "least_load"},
		{"least-load", RoutingProfileLatency, "least_load"},
		{"least-latency", RoutingProfileLatency, "least_latency"},
		{"cost", RoutingProfileCost, "cost_based"},
		{"least_tpm", RoutingProfileThroughput, "least_tpm"},
		{"power-of-two", RoutingProfileBalanced, "power_of_two"},
		{"session", RoutingProfileSticky, "sticky"},
		{"cache", RoutingProfileCache, ""},
		{"least-kv-cache", RoutingProfileCache, "least_kv_cache"},
		{"least-gpu-cache", RoutingProfileCache, "least_gpu_cache"},
		{"round-robin", RoutingProfileBalanced, "round_robin"},
		{"unknown", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			profile, strategy := NormalizeRoutingProfile(tt.raw)
			if profile != tt.wantProfile || strategy != tt.wantStrategy {
				t.Fatalf("NormalizeRoutingProfile(%q) = (%q, %q), want (%q, %q)", tt.raw, profile, strategy, tt.wantProfile, tt.wantStrategy)
			}
		})
	}
}

func TestRouter_RoutingProfileCostOverridesDefaultStrategy(t *testing.T) {
	cfg := config.RouterConfig{Strategy: "round_robin"}
	r := NewRouter(cfg, nil)

	providers := []provider.Provider{
		&mockProvider{name: "expensive", model: "m1", cost: 1.0},
		&mockProvider{name: "cheap", model: "m1", cost: 0.1},
	}
	r.SetProviders(providers)

	ordered, trace := r.ExplainOrderCandidates(providers, RouteContext{RoutingProfile: "cost"})
	if len(ordered) != 2 || ordered[0].Name() != "cheap" {
		t.Fatalf("OrderCandidates(cost profile) = %v, want cheap first", providerNames(ordered))
	}
	if trace.RoutingProfile != RoutingProfileCost || trace.Strategy != "cost_based" {
		t.Fatalf("trace = %+v, want cost/cost_based", trace)
	}
}

func TestRouter_RoutingProfileLatencyOverridesDefaultStrategy(t *testing.T) {
	stats := provider.NewStats()
	p1 := &mockProvider{name: "busy-cheap", model: "m1", cost: 0.1}
	p2 := &mockProvider{name: "idle-expensive", model: "m1", cost: 1.0}
	stats.Register(p1)
	stats.Register(p2)
	stats.IncrementLoad("busy-cheap")

	r := NewRouter(config.RouterConfig{Strategy: "cost_based"}, stats)
	providers := []provider.Provider{p1, p2}
	r.SetProviders(providers)

	ordered, trace := r.ExplainOrderCandidates(providers, RouteContext{RoutingProfile: "latency"})
	if len(ordered) != 2 || ordered[0].Name() != "idle-expensive" {
		t.Fatalf("OrderCandidates(latency profile) = %v, want idle-expensive first", providerNames(ordered))
	}
	if trace.Strategy != "least_load" {
		t.Fatalf("trace.Strategy = %q, want least_load", trace.Strategy)
	}
}

func TestRouter_RoutingProfileStickyWorksWithoutGlobalAffinity(t *testing.T) {
	r := NewRouter(config.RouterConfig{Strategy: "cost_based"}, nil)
	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 0.1},
		&mockProvider{name: "p2", model: "m1", cost: 1.0},
	}
	r.SetProviders(providers)

	ctx := RouteContext{RoutingProfile: "sticky", SessionID: "sess-1"}
	r.PromoteAffinity(ctx, "p2")

	ordered, trace := r.ExplainOrderCandidates(providers, ctx)
	if len(ordered) != 2 || ordered[0].Name() != "p2" {
		t.Fatalf("OrderCandidates(sticky profile) = %v, want p2 first", providerNames(ordered))
	}
	if trace.Affinity != "none+profile:sticky" {
		t.Fatalf("trace.Affinity = %q, want none+profile:sticky", trace.Affinity)
	}
}

func TestRouter_RoutingProfileCachePinsPromptPrefix(t *testing.T) {
	r := NewRouter(config.RouterConfig{
		Strategy: "cost_based",
		Affinity: config.AffinityConfig{
			PrefixDepth: 5,
		},
	}, nil)
	providers := []provider.Provider{
		&mockProvider{name: "cheap", model: "m1", cost: 0.1},
		&mockProvider{name: "cached", model: "m1", cost: 1.0},
	}
	r.SetProviders(providers)

	r.PromoteAffinity(RouteContext{RoutingProfile: "cache", InputText: "hello first prompt"}, "cached")
	ordered, trace := r.ExplainOrderCandidates(providers, RouteContext{RoutingProfile: "cache", InputText: "hello second prompt"})
	if len(ordered) != 2 || ordered[0].Name() != "cached" {
		t.Fatalf("OrderCandidates(cache profile) = %v, want cached first", providerNames(ordered))
	}
	if trace.Strategy != "sticky" || trace.Affinity != "none+profile:cache" {
		t.Fatalf("trace = %+v, want sticky with profile cache affinity", trace)
	}
}

func TestRouter_Sticky(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "sticky",
	}
	r := NewRouter(cfg, nil)

	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 1.0},
		&mockProvider{name: "p3", model: "m3", cost: 1.0},
	}
	r.SetProviders(providers)

	// 同一 session 应该选择同一个 provider
	p1 := r.Select("model1", "session-abc")
	p2 := r.Select("model1", "session-abc")
	p3 := r.Select("model1", "session-abc")

	if p1.Name() != p2.Name() || p2.Name() != p3.Name() {
		t.Errorf("sticky routing failed: %s != %s != %s", p1.Name(), p2.Name(), p3.Name())
	}

	// 不同 session 可能不同
	p4 := r.Select("model1", "session-xyz")
	_ = p4 // 只保证不 panic
}

func TestRouter_StickyEmptySession(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "sticky",
	}
	r := NewRouter(cfg, nil)

	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 1.0},
	}
	r.SetProviders(providers)

	// 空 session 应该不 panic，回退到轮询
	p := r.Select("model1", "")
	if p == nil {
		t.Error("empty session should fallback to round robin")
	}
}

func TestRouter_EmptyProviders(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
	}
	r := NewRouter(cfg, nil)
	r.SetProviders(nil)

	p := r.Select("model1", "")
	if p != nil {
		t.Error("should return nil for empty providers")
	}
}

func TestRouter_SelectFrom(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
	}
	r := NewRouter(cfg, nil)

	allProviders := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 1.0},
		&mockProvider{name: "p3", model: "m3", cost: 1.0},
	}
	r.SetProviders(allProviders)

	// 只从候选列表中选择
	candidates := []provider.Provider{allProviders[0], allProviders[2]}
	p := r.SelectFrom(candidates, "")

	if p.Name() != "p1" && p.Name() != "p3" {
		t.Errorf("should select from candidates only, got %s", p.Name())
	}
}

func TestRouter_OrderCandidatesRuleEngineFiltersProviders(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "least_load",
		RuleEngine: config.RuleEngineConfig{
			Enabled: true,
			Rules: []config.RouteRuleConfig{{
				Name: "long-context-tools",
				Match: config.RouteMatchConfig{
					MinPromptTokens: 100,
					HasTools:        boolPtr(true),
				},
				Action: config.RouteActionConfig{
					Providers: []string{"p2", "p3"},
				},
			}},
		},
	}
	r := NewRouter(cfg, nil)
	r.SetProviders([]provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 0.5},
		&mockProvider{name: "p3", model: "m3", cost: 0.7},
	})

	ordered := r.OrderCandidates(r.List(), RouteContext{
		PromptTokens: 150,
		HasTools:     true,
	})
	if len(ordered) != 2 || ordered[0].Name() != "p2" || ordered[1].Name() != "p3" {
		t.Fatalf("OrderCandidates(rule engine) = %v, want [p2 p3]", providerNames(ordered))
	}
}

func TestRouter_OrderCandidatesRuleEngineFiltersProviderLabels(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
		RuleEngine: config.RuleEngineConfig{
			Enabled: true,
			Rules: []config.RouteRuleConfig{{
				Name:  "gpu-route",
				Match: config.RouteMatchConfig{Models: []string{"qwen"}},
				Action: config.RouteActionConfig{
					ProviderLabels: map[string]string{"accelerator": "h100"},
				},
			}},
		},
	}
	r := NewRouter(cfg, nil)
	r.SetProviders([]provider.Provider{
		&mockProvider{name: "cpu", model: "qwen", labels: map[string]string{"accelerator": "cpu"}},
		&mockProvider{name: "gpu", model: "qwen", labels: map[string]string{"accelerator": "h100"}},
	})

	ordered := r.OrderCandidates(r.List(), RouteContext{Model: "qwen"})
	if len(ordered) != 1 || ordered[0].Name() != "gpu" {
		t.Fatalf("OrderCandidates(label selector) = %v, want gpu", providerNames(ordered))
	}
}

func TestRouter_OrderCandidatesRuleEngineRegexMatch(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
		RuleEngine: config.RuleEngineConfig{
			Enabled: true,
			Rules: []config.RouteRuleConfig{{
				Name: "code-traffic",
				Match: config.RouteMatchConfig{
					AnyRegex: []string{`(?i)stack trace`, `(?i)golang`},
				},
				Action: config.RouteActionConfig{
					Providers: []string{"coder"},
				},
			}},
		},
	}
	r := NewRouter(cfg, nil)
	r.SetProviders([]provider.Provider{
		&mockProvider{name: "general", model: "m1", cost: 1.0},
		&mockProvider{name: "coder", model: "m2", cost: 1.0},
	})

	ordered := r.OrderCandidates(r.List(), RouteContext{
		InputText: "Please debug this Go stack trace for me.",
	})
	if len(ordered) != 1 || ordered[0].Name() != "coder" {
		t.Fatalf("OrderCandidates(regex rule) = %v, want [coder]", providerNames(ordered))
	}
}

func TestRouter_OrderCandidatesRuleEngineStructuredOutput(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
		RuleEngine: config.RuleEngineConfig{
			Enabled: true,
			Rules: []config.RouteRuleConfig{{
				Name: "structured-output",
				Match: config.RouteMatchConfig{
					HasStructuredOutput: boolPtr(true),
				},
				Action: config.RouteActionConfig{
					Providers: []string{"json-safe"},
				},
			}},
		},
	}
	r := NewRouter(cfg, nil)
	r.SetProviders([]provider.Provider{
		&mockProvider{name: "general", model: "m1", cost: 1.0},
		&mockProvider{name: "json-safe", model: "m2", cost: 1.0},
	})

	ordered := r.OrderCandidates(r.List(), RouteContext{
		HasStructuredOutput: true,
	})
	if len(ordered) != 1 || ordered[0].Name() != "json-safe" {
		t.Fatalf("OrderCandidates(structured rule) = %v, want [json-safe]", providerNames(ordered))
	}
}

func TestRouter_OrderCandidatesMLRankPlaceholderIsNoop(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
		Ranker: config.RankerConfig{
			Enabled: true,
			Method:  "ml_rank",
		},
	}
	r := NewRouter(cfg, nil)
	r.SetProviders([]provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 1.0},
	})

	ordered := r.OrderCandidates(r.List(), RouteContext{})
	if len(ordered) != 2 || ordered[0].Name() != "p1" || ordered[1].Name() != "p2" {
		t.Fatalf("OrderCandidates(ml_rank ranker placeholder) = %v, want [p1 p2]", providerNames(ordered))
	}
}

func TestRouter_StrategyMLRankAliasIsNoop(t *testing.T) {
	cfg := config.RouterConfig{Strategy: "ml_rank"}
	r := NewRouter(cfg, nil)
	r.SetProviders([]provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 1.0},
	})

	ordered := r.OrderCandidates(r.List(), RouteContext{})
	if len(ordered) != 2 || ordered[0].Name() != "p1" || ordered[1].Name() != "p2" {
		t.Fatalf("OrderCandidates(strategy ml_rank alias) = %v, want [p1 p2]", providerNames(ordered))
	}
}

func (r *Router) List() []provider.Provider {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]provider.Provider, len(r.providers))
	copy(result, r.providers)
	return result
}

func providerNames(providers []provider.Provider) []string {
	result := make([]string, 0, len(providers))
	for _, p := range providers {
		result = append(result, p.Name())
	}
	return result
}

func newMetricsServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func waitForInferenceState(t *testing.T, scraper *InferenceScraper, name string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if state, ok := scraper.Get(name); ok && !state.Stale {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for inference state %s", name)
}

func boolPtr(v bool) *bool {
	return &v
}
