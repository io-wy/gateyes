package router

import (
	"testing"

	"github.com/gateyes/gateway/internal/service/provider"
)

func TestSessionAffinity_PinSameSession(t *testing.T) {
	sa := NewSessionAffinity(0)
	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
		&mockProvider{name: "p3"},
	}
	ctx := RouteContext{SessionID: "sess-1"}
	ordered := sa.Pin(candidates, ctx)
	first := ordered[0].Name()

	// Same session should always pin to the same provider
	for i := 0; i < 10; i++ {
		o := sa.Pin(candidates, ctx)
		if o[0].Name() != first {
			t.Fatalf("session affinity should be stable, got %s then %s", first, o[0].Name())
		}
	}
}

func TestSessionAffinity_PinDifferentSessions(t *testing.T) {
	sa := NewSessionAffinity(0)
	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
		&mockProvider{name: "p3"},
	}
	counts := map[string]int{"p1": 0, "p2": 0, "p3": 0}
	for i := 0; i < 100; i++ {
		ctx := RouteContext{SessionID: string(rune('a' + i))}
		ordered := sa.Pin(candidates, ctx)
		counts[ordered[0].Name()]++
	}
	// With 100 different sessions and 3 providers, each should get some
	for name, c := range counts {
		if c == 0 {
			t.Fatalf("provider %s never picked across 100 sessions", name)
		}
	}
}

func TestSessionAffinity_PinEmptySession(t *testing.T) {
	sa := NewSessionAffinity(0)
	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
	}
	ordered := sa.Pin(candidates, RouteContext{SessionID: ""})
	if ordered[0].Name() != "p1" {
		t.Fatalf("empty session should not change order")
	}
}

func TestSessionAffinity_PinSingleCandidate(t *testing.T) {
	sa := NewSessionAffinity(0)
	candidates := []provider.Provider{&mockProvider{name: "p1"}}
	ordered := sa.Pin(candidates, RouteContext{SessionID: "sess"})
	if len(ordered) != 1 || ordered[0].Name() != "p1" {
		t.Fatalf("single candidate should be unchanged")
	}
}

func TestSessionAffinity_PromoteOverrides(t *testing.T) {
	sa := NewSessionAffinity(0)
	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
	}
	ctx := RouteContext{SessionID: "sess-1"}
	sa.Pin(candidates, ctx) // establish initial mapping
	sa.Promote(ctx, "p2")
	ordered := sa.Pin(candidates, ctx)
	if ordered[0].Name() != "p2" {
		t.Fatalf("promote should override pinned provider, got %s", ordered[0].Name())
	}
}

func TestSessionAffinity_PromoteEmptySession(t *testing.T) {
	sa := NewSessionAffinity(0)
	// Should not panic
	sa.Promote(RouteContext{SessionID: ""}, "p1")
}
