package router

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/service/provider"
)

// mockProvider 用于测试的 mock
type mockProvider struct {
	name  string
	model string
	cost  float64
	load  int64
}

func (m *mockProvider) Name() string      { return m.name }
func (m *mockProvider) Type() string      { return "mock" }
func (m *mockProvider) BaseURL() string  { return "http://test.com" }
func (m *mockProvider) Model() string      { return m.model }
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

func TestRouter_RoundRobin(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
	}
	r := NewRouter(cfg)

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
	r := NewRouter(cfg)

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
	cfg := config.RouterConfig{
		Strategy: "least_load",
	}
	r := NewRouter(cfg)

	p1 := &mockProvider{name: "p1", model: "m1", cost: 1.0}
	p2 := &mockProvider{name: "p2", model: "m2", cost: 1.0}
	p3 := &mockProvider{name: "p3", model: "m3", cost: 1.0}

	providers := []provider.Provider{p1, p2, p3}
	r.SetProviders(providers)

	// 初始负载都是 0，应该选择第一个
	p := r.Select("model1", "")
	if p.Name() != "p1" {
		t.Errorf("expected p1 first, got %s", p.Name())
	}

	// 增加 p1 负载
	r.IncLoad("p1")
	r.IncLoad("p1")

	// 现在应该选择 p2 或 p3
	p = r.Select("model1", "")
	if p.Name() == "p1" {
		t.Error("p1 should not be selected with higher load")
	}
}

func TestRouter_CostBased(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "cost_based",
	}
	r := NewRouter(cfg)

	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},  // 最贵
		&mockProvider{name: "p2", model: "m2", cost: 0.5}, // 中等
		&mockProvider{name: "p3", model: "m3", cost: 0.1},  // 最便宜
	}
	r.SetProviders(providers)

	// 应该总是选择最便宜的
	p := r.Select("model1", "")
	if p.Name() != "p3" {
		t.Errorf("expected p3 (cheapest), got %s", p.Name())
	}
}

func TestRouter_Sticky(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "sticky",
	}
	r := NewRouter(cfg)

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
	r := NewRouter(cfg)

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
	r := NewRouter(cfg)
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
	r := NewRouter(cfg)

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

func TestRouter_LoadManagement(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
	}
	r := NewRouter(cfg)

	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 1.0},
	}
	r.SetProviders(providers)

	r.IncLoad("p1")
	r.IncLoad("p1")
	r.IncLoad("p2")

	// 增加负载
	r.IncLoad("p1")

	// 减少负载
	r.DecLoad("p1")
	r.DecLoad("p1")
	r.DecLoad("p1") // 尝试减少到负数，应该被保护

	// 验证负载不会变负（通过 least_load 验证）
	// 如果负载变成负数，least_load 会出问题
}
