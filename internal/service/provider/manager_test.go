package provider

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
)

type closableProviderStub struct {
	closed bool
}

func (p *closableProviderStub) Name() string                                    { return "closable" }
func (p *closableProviderStub) Type() string                                    { return "test" }
func (p *closableProviderStub) BaseURL() string                                 { return "" }
func (p *closableProviderStub) Model() string                                   { return "" }
func (p *closableProviderStub) UnitCost() float64                               { return 0 }
func (p *closableProviderStub) Cost(promptTokens, completionTokens int) float64 { return 0 }
func (p *closableProviderStub) CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error) {
	return nil, nil
}
func (p *closableProviderStub) StreamResponse(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, <-chan error) {
	return nil, nil
}
func (p *closableProviderStub) CreateEmbedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	return nil, nil
}
func (p *closableProviderStub) Weight() int { return 0 }
func (p *closableProviderStub) CloseIdleConnections() {
	p.closed = true
}

func TestManagerStatsAndFactoryHelpers(t *testing.T) {
	cfgs := []config.ProviderConfig{
		{
			Name:        "openai-a",
			Type:        "openai",
			BaseURL:     "https://openai.example",
			APIKey:      "k1",
			Model:       "gpt-test",
			PriceInput:  0.1,
			PriceOutput: 0.2,
			Timeout:     5,
			Enabled:     true,
		},
		{
			Name:      "anthropic-a",
			Type:      "anthropic",
			BaseURL:   "https://anthropic.example",
			APIKey:    "k2",
			Model:     "claude-test",
			Timeout:   5,
			MaxTokens: 256,
			Enabled:   true,
		},
		{Name: "disabled", Type: "openai", Enabled: false},
	}

	manager, err := NewManager(cfgs)
	if err != nil {
		t.Fatalf("NewManager() error: %v", err)
	}
	if _, ok := manager.Get("openai-a"); !ok {
		t.Fatal("Manager.Get(openai-a) = false, want true")
	}
	if _, ok := manager.Get("disabled"); ok {
		t.Fatal("Manager.Get(disabled) = true, want false")
	}
	if got := len(manager.List()); got != 2 {
		t.Fatalf("len(Manager.List()) = %d, want %d", got, 2)
	}
	if got := len(manager.ListByNames([]string{"anthropic-a", "missing", "openai-a"})); got != 2 {
		t.Fatalf("len(Manager.ListByNames()) = %d, want %d", got, 2)
	}
	if _, err := newProvider(config.ProviderConfig{Name: "bad", Type: "unsupported"}); err == nil {
		t.Fatal("newProvider(unsupported) error = nil, want non-nil")
	}

	manager.CloseIdleConnections()

	stats := NewStats()
	p := NewOpenAIProvider(cfgs[0])
	stats.Register(p)
	stats.RecordRequest("openai-a", true, 10, 20)
	stats.RecordRequest("openai-a", false, 5, 40)
	stats.IncrementLoad("openai-a")
	stats.DecrementLoad("openai-a")

	item, ok := stats.Get("openai-a")
	if !ok {
		t.Fatal("Stats.Get(openai-a) = false, want true")
	}
	if item.TotalRequests != 2 || item.SuccessRequests != 1 || item.FailedRequests != 1 || item.TotalTokens != 15 {
		t.Fatalf("Stats.Get(openai-a) = %+v, want totals 2/1/1/15", item)
	}
	if got := len(stats.List()); got != 1 {
		t.Fatalf("len(Stats.List()) = %d, want %d", got, 1)
	}
	total, success, failed, tokens, avgLatency := stats.GlobalStats()
	if total != 2 || success != 1 || failed != 1 || tokens != 15 || avgLatency != 30 {
		t.Fatalf("Stats.GlobalStats() = (%d,%d,%d,%d,%v), want (2,1,1,15,30)", total, success, failed, tokens, avgLatency)
	}
}

func TestManagerUpsertAndRemoveRuntimeProvider(t *testing.T) {
	manager, err := NewManager([]config.ProviderConfig{{
		Name:      "openai-a",
		Type:      "openai",
		BaseURL:   "https://openai.example/v1",
		Endpoint:  "chat",
		APIKey:    "k1",
		Model:     "model-a",
		Timeout:   5,
		MaxTokens: 128,
		Enabled:   true,
	}})
	if err != nil {
		t.Fatalf("NewManager() error: %v", err)
	}

	record := repository.ProviderRegistryRecord{
		Name:          "runtime-openai",
		Type:          "openai",
		BaseURL:       "https://runtime.example/v1",
		Endpoint:      "responses",
		Model:         "runtime-model",
		Enabled:       true,
		HealthStatus:  ProviderHealthHealthy,
		RoutingWeight: 3,
		RuntimeConfig: &repository.ProviderRuntimeConfig{
			APIKey:      "runtime-key",
			Timeout:     5,
			MaxTokens:   256,
			Enabled:     true,
			PriceInput:  0.1,
			PriceOutput: 0.2,
		},
	}
	if err := manager.UpsertRuntimeProvider(record); err != nil {
		t.Fatalf("UpsertRuntimeProvider(create) error: %v", err)
	}
	if got, ok := manager.Get("runtime-openai"); !ok || got.Model() != "runtime-model" {
		t.Fatalf("Manager.Get(runtime-openai) = (%v,%v), want runtime provider instance", got, ok)
	}

	record.Model = "runtime-model-v2"
	record.RuntimeConfig.MaxTokens = 512
	if err := manager.UpsertRuntimeProvider(record); err != nil {
		t.Fatalf("UpsertRuntimeProvider(update) error: %v", err)
	}
	if got, ok := manager.Get("runtime-openai"); !ok || got.Model() != "runtime-model-v2" {
		t.Fatalf("Manager.Get(runtime-openai after update) = (%v,%v), want updated model", got, ok)
	}

	manager.RemoveRuntimeProvider("runtime-openai")
	if _, ok := manager.Get("runtime-openai"); ok {
		t.Fatal("Manager.Get(runtime-openai after delete) ok = true, want false")
	}
	if _, ok := manager.Registry("runtime-openai"); ok {
		t.Fatal("Manager.Registry(runtime-openai after delete) ok = true, want false")
	}
}

func TestManagerCloseIdleConnectionsClosesClosableProviders(t *testing.T) {
	prov := &closableProviderStub{}
	manager := &Manager{
		providers: map[string]Provider{
			"closable": prov,
		},
		Stats: NewStats(),
	}

	manager.CloseIdleConnections()
	if !prov.closed {
		t.Fatal("CloseIdleConnections() did not close idle connections on closable provider")
	}
}
