package router

import (
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/service/provider"
)

// TestRouter_Pipeline_AffinityBeforeStrategy verifies the pipeline ordering:
// ruleEngine → ranker → affinity → strategy.
// We use ExplainOrderCandidates to inspect the intermediate state after each
// step. Affinity should pin p3 to the front *before* strategy runs.
func TestRouter_Pipeline_AffinityBeforeStrategy(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "random",
		Affinity: config.AffinityConfig{
			Enabled: true,
		},
	}
	r := NewRouter(cfg, nil)
	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 1.0},
		&mockProvider{name: "p3", model: "m3", cost: 1.0},
	}
	r.SetProviders(providers)

	// Promote p3 for this session
	ctx := RouteContext{SessionID: "sess-pipeline"}
	r.PromoteAffinity(ctx, "p3")

	// AfterAffinity must show p3 at the front; strategy may reorder afterward.
	ordered, trace := r.ExplainOrderCandidates(providers, ctx)
	if len(trace.AfterAffinity) != 3 || trace.AfterAffinity[0] != "p3" {
		t.Fatalf("affinity should pin p3 before strategy runs, AfterAffinity=%v", trace.AfterAffinity)
	}
	_ = ordered // final order may differ due to random strategy
}

// TestRouter_Pipeline_SessionAffinityStickyCompat verifies that the legacy
// "sticky" strategy config still works via the affinity layer.
func TestRouter_Pipeline_SessionAffinityStickyCompat(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "sticky",
	}
	r := NewRouter(cfg, nil)
	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 1.0},
	}
	r.SetProviders(providers)

	// First call establishes the sticky mapping
	p1 := r.Select("m1", "sticky-sess")
	// Subsequent calls should return the same provider
	for i := 0; i < 10; i++ {
		p := r.Select("m1", "sticky-sess")
		if p.Name() != p1.Name() {
			t.Fatalf("sticky compat failed: %s != %s", p.Name(), p1.Name())
		}
	}
}

// TestRouter_Pipeline_PrefixAffinityWithRuleEngine verifies that prefix
// affinity works correctly even when ruleEngine filters the candidate set.
func TestRouter_Pipeline_PrefixAffinityWithRuleEngine(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
		RuleEngine: config.RuleEngineConfig{
			Enabled: true,
			Rules: []config.RouteRuleConfig{{
				Name: "code-only",
				Match: config.RouteMatchConfig{
					AnyRegex: []string{"(?i)code"},
				},
				Action: config.RouteActionConfig{
					Providers: []string{"coder"},
				},
			}},
		},
		Affinity: config.AffinityConfig{
			Enabled:     true,
			PrefixDepth: 5,
		},
	}
	r := NewRouter(cfg, nil)
	providers := []provider.Provider{
		&mockProvider{name: "general", model: "m1", cost: 1.0},
		&mockProvider{name: "coder", model: "m2", cost: 1.0},
	}
	r.SetProviders(providers)

	// Promote prefix affinity for "write code"
	r.PromoteAffinity(RouteContext{InputText: "write code for me"}, "coder")

	// Rule engine filters to [coder]; prefix affinity should still work
	ordered := r.OrderCandidates(providers, RouteContext{
		InputText: "write code quickly",
	})
	if len(ordered) != 1 || ordered[0].Name() != "coder" {
		t.Fatalf("expected [coder], got %v", providerNames(ordered))
	}
}

// TestRouter_ExplainOrderCandidates_WithAffinity verifies the trace
// correctly reflects affinity transformations.
func TestRouter_ExplainOrderCandidates_WithAffinity(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
		Affinity: config.AffinityConfig{
			Enabled: true,
		},
	}
	r := NewRouter(cfg, nil)
	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 1.0},
	}
	r.SetProviders(providers)

	r.PromoteAffinity(RouteContext{SessionID: "sess-explain"}, "p2")

	ordered, trace := r.ExplainOrderCandidates(providers, RouteContext{SessionID: "sess-explain"})
	if len(ordered) != 2 || ordered[0].Name() != "p2" {
		t.Fatalf("expected p2 first, got %v", providerNames(ordered))
	}
	if trace.Affinity != "composite" {
		t.Fatalf("expected affinity=composite, got %s", trace.Affinity)
	}
	if len(trace.AfterAffinity) != 2 || trace.AfterAffinity[0] != "p2" {
		t.Fatalf("trace.AfterAffinity should show p2 first, got %v", trace.AfterAffinity)
	}
}

// TestRouter_PromoteAffinity_UpdatesState verifies that PromoteAffinity
// correctly updates the internal affinity state and subsequent selects
// reflect the new mapping.
func TestRouter_PromoteAffinity_UpdatesState(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "least_load",
		Affinity: config.AffinityConfig{
			Enabled: true,
		},
	}
	r := NewRouter(cfg, nil)
	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "m1", cost: 1.0},
		&mockProvider{name: "p2", model: "m2", cost: 1.0},
	}
	r.SetProviders(providers)

	ctx := RouteContext{SessionID: "sess-promote"}

	// Before promote: least_load picks p1 (first, equal load)
	p := r.SelectFromWithContext(providers, ctx)
	if p.Name() != "p1" {
		t.Logf("initial selection: %s", p.Name())
	}

	// Promote p2
	r.PromoteAffinity(ctx, "p2")

	// After promote: affinity should override and pin p2
	for i := 0; i < 5; i++ {
		p = r.SelectFromWithContext(providers, ctx)
		if p.Name() != "p2" {
			t.Fatalf("after promote expected p2, got %s", p.Name())
		}
	}
}

// TestRouter_Affinity_ModelMatchAfterPin verifies that when affinity pins
// a provider, the model match filter still applies (i.e., a pinned provider
// with wrong model is skipped).
func TestRouter_Affinity_ModelMatchAfterPin(t *testing.T) {
	cfg := config.RouterConfig{
		Strategy: "round_robin",
		Affinity: config.AffinityConfig{
			Enabled: true,
		},
	}
	r := NewRouter(cfg, nil)
	providers := []provider.Provider{
		&mockProvider{name: "p1", model: "gpt-4", cost: 1.0},
		&mockProvider{name: "p2", model: "claude-3", cost: 1.0},
	}
	r.SetProviders(providers)

	// Pin p2 (claude-3) for this session
	r.PromoteAffinity(RouteContext{SessionID: "sess-model"}, "p2")

	// Request gpt-4 model: affinity pins p2 to front, but model filter
	// skips it because p2.Model() != "gpt-4", so p1 should be selected.
	p := r.Select("gpt-4", "sess-model")
	if p.Name() != "p1" {
		t.Fatalf("model filter should skip pinned provider with wrong model, got %s", p.Name())
	}
}
