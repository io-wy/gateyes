package router

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/service/provider"
)

// benchProvider is a lightweight provider implementation for benchmarks.
type benchProvider struct {
	name  string
	model string
	cost  float64
	wgt   int
}

func (m *benchProvider) Name() string      { return m.name }
func (m *benchProvider) Type() string      { return "mock" }
func (m *benchProvider) BaseURL() string   { return "http://test.com" }
func (m *benchProvider) Model() string     { return m.model }
func (m *benchProvider) Weight() int       { return m.wgt }
func (m *benchProvider) UnitCost() float64 { return m.cost }
func (m *benchProvider) Cost(prompt, completion int) float64 {
	return float64(prompt+completion) * m.cost
}
func (m *benchProvider) CreateResponse(ctx context.Context, req *provider.ResponseRequest) (*provider.Response, error) {
	return nil, nil
}
func (m *benchProvider) StreamResponse(ctx context.Context, req *provider.ResponseRequest) (<-chan provider.ResponseEvent, <-chan error) {
	return nil, nil
}
func (m *benchProvider) CreateEmbedding(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, nil
}

func makeBenchProviders(n int) []provider.Provider {
	providers := make([]provider.Provider, n)
	for i := 0; i < n; i++ {
		providers[i] = &benchProvider{
			name:  "p" + string(rune('1'+i)),
			model: "m" + string(rune('1'+i)),
			cost:  1.0,
			wgt:   1,
		}
	}
	return providers
}

func BenchmarkRouter_Select(b *testing.B) {
	cfg := config.RouterConfig{Strategy: "round_robin"}
	r := NewRouter(cfg, nil)
	providers := makeBenchProviders(3)
	r.SetProviders(providers)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Select("m1", "")
	}
}

func BenchmarkRouter_Select_Parallel(b *testing.B) {
	cfg := config.RouterConfig{Strategy: "round_robin"}
	r := NewRouter(cfg, nil)
	providers := makeBenchProviders(3)
	r.SetProviders(providers)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = r.Select("m1", "")
		}
	})
}

func BenchmarkRouter_Select_Sticky(b *testing.B) {
	cfg := config.RouterConfig{Strategy: "sticky"}
	r := NewRouter(cfg, nil)
	providers := makeBenchProviders(3)
	r.SetProviders(providers)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Select("m1", "session-abc")
	}
}

func BenchmarkRouter_Select_Sticky_Parallel(b *testing.B) {
	cfg := config.RouterConfig{Strategy: "sticky"}
	r := NewRouter(cfg, nil)
	providers := makeBenchProviders(3)
	r.SetProviders(providers)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = r.Select("m1", "session-abc")
		}
	})
}

func BenchmarkRouter_OrderCandidates(b *testing.B) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
		RuleEngine: config.RuleEngineConfig{
			Enabled: true,
			Rules: []config.RouteRuleConfig{{
				Name: "long-context",
				Match: config.RouteMatchConfig{
					MinPromptTokens: 100,
				},
				Action: config.RouteActionConfig{
					Providers: []string{"p2", "p3"},
				},
			}},
		},
		Ranker: config.RankerConfig{
			Enabled: true,
			Method:  "ml_rank",
		},
		Affinity: config.AffinityConfig{
			Enabled:     true,
			SessionTTL:  300,
			PrefixTTL:   300,
			PrefixDepth: 10,
		},
	}
	stats := provider.NewStats()
	r := NewRouter(cfg, stats)
	providers := makeBenchProviders(3)
	r.SetProviders(providers)
	for _, p := range providers {
		stats.Register(p)
	}

	ctx := RouteContext{
		Model:        "m1",
		SessionID:    "session-abc",
		InputText:    "hello world this is a test prompt",
		PromptTokens: 150,
		Stream:       false,
		HasTools:     true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.OrderCandidates(providers, ctx)
	}
}

func BenchmarkRouter_OrderCandidates_Parallel(b *testing.B) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
		RuleEngine: config.RuleEngineConfig{
			Enabled: true,
			Rules: []config.RouteRuleConfig{{
				Name: "long-context",
				Match: config.RouteMatchConfig{
					MinPromptTokens: 100,
				},
				Action: config.RouteActionConfig{
					Providers: []string{"p2", "p3"},
				},
			}},
		},
		Ranker: config.RankerConfig{
			Enabled: true,
			Method:  "ml_rank",
		},
		Affinity: config.AffinityConfig{
			Enabled:     true,
			SessionTTL:  300,
			PrefixTTL:   300,
			PrefixDepth: 10,
		},
	}
	stats := provider.NewStats()
	r := NewRouter(cfg, stats)
	providers := makeBenchProviders(3)
	r.SetProviders(providers)
	for _, p := range providers {
		stats.Register(p)
	}

	ctx := RouteContext{
		Model:        "m1",
		SessionID:    "session-abc",
		InputText:    "hello world this is a test prompt",
		PromptTokens: 150,
		Stream:       false,
		HasTools:     true,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = r.OrderCandidates(providers, ctx)
		}
	})
}

func BenchmarkSessionAffinity_Pin(b *testing.B) {
	sa := NewSessionAffinity(0)
	providers := makeBenchProviders(3)

	// Pre-promote 100 sessions
	for i := 0; i < 100; i++ {
		ctx := RouteContext{SessionID: "session-" + string(rune('0'+i%10)) + string(rune('0'+i/10))}
		sa.Promote(ctx, providers[i%len(providers)].Name())
	}

	ctx := RouteContext{SessionID: "session-42"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sa.Pin(providers, ctx)
	}
}

func BenchmarkSessionAffinity_Pin_Parallel(b *testing.B) {
	sa := NewSessionAffinity(0)
	providers := makeBenchProviders(3)

	for i := 0; i < 100; i++ {
		ctx := RouteContext{SessionID: "session-" + string(rune('0'+i%10)) + string(rune('0'+i/10))}
		sa.Promote(ctx, providers[i%len(providers)].Name())
	}

	ctx := RouteContext{SessionID: "session-42"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = sa.Pin(providers, ctx)
		}
	})
}

func BenchmarkPrefixAffinity_Pin(b *testing.B) {
	pa := NewPrefixAffinity(10, 0)
	providers := makeBenchProviders(3)

	// Pre-promote 100 prefixes
	for i := 0; i < 100; i++ {
		ctx := RouteContext{InputText: "prefix-" + string(rune('a'+i%26)) + " test prompt content"}
		pa.Promote(ctx, providers[i%len(providers)].Name())
	}

	ctx := RouteContext{InputText: "prefix-x test prompt content"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pa.Pin(providers, ctx)
	}
}

func BenchmarkPrefixAffinity_Pin_Parallel(b *testing.B) {
	pa := NewPrefixAffinity(10, 0)
	providers := makeBenchProviders(3)

	for i := 0; i < 100; i++ {
		ctx := RouteContext{InputText: "prefix-" + string(rune('a'+i%26)) + " test prompt content"}
		pa.Promote(ctx, providers[i%len(providers)].Name())
	}

	ctx := RouteContext{InputText: "prefix-x test prompt content"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = pa.Pin(providers, ctx)
		}
	})
}

func BenchmarkCompositeAffinity_Pin(b *testing.B) {
	sa := NewSessionAffinity(0)
	pa := NewPrefixAffinity(10, 0)
	ca := NewCompositeAffinity(sa, pa)
	providers := makeBenchProviders(3)

	// Pre-promote 50 sessions and 50 prefixes
	for i := 0; i < 50; i++ {
		ctx := RouteContext{SessionID: "session-" + string(rune('0'+i%10)) + string(rune('0'+i/10))}
		sa.Promote(ctx, providers[i%len(providers)].Name())
	}
	for i := 0; i < 50; i++ {
		ctx := RouteContext{InputText: "prefix-" + string(rune('a'+i%26)) + " test prompt content"}
		pa.Promote(ctx, providers[i%len(providers)].Name())
	}

	ctx := RouteContext{
		SessionID: "session-25",
		InputText: "prefix-m test prompt content",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ca.Pin(providers, ctx)
	}
}

func BenchmarkCompositeAffinity_Pin_Parallel(b *testing.B) {
	sa := NewSessionAffinity(0)
	pa := NewPrefixAffinity(10, 0)
	ca := NewCompositeAffinity(sa, pa)
	providers := makeBenchProviders(3)

	for i := 0; i < 50; i++ {
		ctx := RouteContext{SessionID: "session-" + string(rune('0'+i%10)) + string(rune('0'+i/10))}
		sa.Promote(ctx, providers[i%len(providers)].Name())
	}
	for i := 0; i < 50; i++ {
		ctx := RouteContext{InputText: "prefix-" + string(rune('a'+i%26)) + " test prompt content"}
		pa.Promote(ctx, providers[i%len(providers)].Name())
	}

	ctx := RouteContext{
		SessionID: "session-25",
		InputText: "prefix-m test prompt content",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = ca.Pin(providers, ctx)
		}
	})
}
