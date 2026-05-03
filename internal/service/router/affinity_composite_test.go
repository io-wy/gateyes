package router

import (
	"sync"
	"testing"

	"github.com/gateyes/gateway/internal/service/provider"
)

// TestCompositeAffinity_FirstPinWins verifies that when the first child
// affinity produces a pin, subsequent children are not consulted.
func TestCompositeAffinity_FirstPinWins(t *testing.T) {
	sa := NewSessionAffinity(0)
	pa := NewPrefixAffinity(10, 0)
	comp := NewCompositeAffinity(sa, pa)

	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
		&mockProvider{name: "p3"},
	}

	// Promote via session affinity
	ctx := RouteContext{SessionID: "sess-1"}
	comp.Promote(ctx, "p3")

	// Also promote via prefix affinity
	ctx2 := RouteContext{InputText: "hello world"}
	comp.Promote(ctx2, "p2")

	// When both session and prefix context are present, session (first)
	// should win because it was promoted to p3.
	ctxBoth := RouteContext{SessionID: "sess-1", InputText: "hello world"}
	ordered := comp.Pin(candidates, ctxBoth)
	if ordered[0].Name() != "p3" {
		t.Fatalf("first-pin-wins: expected p3, got %s", ordered[0].Name())
	}
}

// TestCompositeAffinity_SecondGetsChanceIfFirstNoop verifies that when
// the first child returns candidates unchanged, the second child gets a chance.
func TestCompositeAffinity_SecondGetsChanceIfFirstNoop(t *testing.T) {
	// Session affinity with empty session is a noop
	sa := NewSessionAffinity(0)
	pa := NewPrefixAffinity(10, 0)
	comp := NewCompositeAffinity(sa, pa)

	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
	}

	// Promote prefix affinity
	ctx := RouteContext{InputText: "hello world"}
	comp.Promote(ctx, "p2")

	// Empty session means session affinity is noop, prefix should pin
	ordered := comp.Pin(candidates, RouteContext{SessionID: "", InputText: "hello world"})
	if ordered[0].Name() != "p2" {
		t.Fatalf("second should pin when first is noop, got %s", ordered[0].Name())
	}
}

// TestCompositeAffinity_EmptyChain returns candidates unchanged.
func TestCompositeAffinity_EmptyChain(t *testing.T) {
	comp := NewCompositeAffinity()
	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
	}
	ordered := comp.Pin(candidates, RouteContext{})
	if len(ordered) != 2 || ordered[0].Name() != "p1" {
		t.Fatalf("empty chain should be identity")
	}
}

// TestCompositeAffinity_SingleCandidate short-circuits.
func TestCompositeAffinity_SingleCandidate(t *testing.T) {
	sa := NewSessionAffinity(0)
	comp := NewCompositeAffinity(sa)
	candidates := []provider.Provider{&mockProvider{name: "p1"}}
	comp.Promote(RouteContext{SessionID: "s"}, "p1")
	ordered := comp.Pin(candidates, RouteContext{SessionID: "s"})
	if len(ordered) != 1 || ordered[0].Name() != "p1" {
		t.Fatalf("single candidate should be unchanged")
	}
}

// TestCompositeAffinity_PromoteToAllChildren verifies Promote forwards
// to every child in the chain.
func TestCompositeAffinity_PromoteToAllChildren(t *testing.T) {
	sa := NewSessionAffinity(0)
	pa := NewPrefixAffinity(10, 0)
	comp := NewCompositeAffinity(sa, pa)

	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
	}

	ctx := RouteContext{SessionID: "sess-1", InputText: "hello world"}
	comp.Promote(ctx, "p2")

	// Both children should have received the promote
	orderedSess := sa.Pin(candidates, RouteContext{SessionID: "sess-1"})
	if orderedSess[0].Name() != "p2" {
		t.Fatalf("session affinity should be promoted")
	}
	orderedPref := pa.Pin(candidates, RouteContext{InputText: "hello world"})
	if orderedPref[0].Name() != "p2" {
		t.Fatalf("prefix affinity should be promoted")
	}
}

// TestCompositeAffinity_ConcurrentPin verifies thread-safe operation
// when multiple goroutines call Pin concurrently.
func TestCompositeAffinity_ConcurrentPin(t *testing.T) {
	sa := NewSessionAffinity(0)
	pa := NewPrefixAffinity(10, 0)
	comp := NewCompositeAffinity(sa, pa)

	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
		&mockProvider{name: "p3"},
	}

	// Pre-promote some mappings
	for i := 0; i < 100; i++ {
		comp.Promote(RouteContext{SessionID: string(rune('a' + i%26))}, "p2")
		comp.Promote(RouteContext{InputText: string(rune('a' + i%26)) + " world"}, "p3")
	}

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx := RouteContext{
				SessionID:   string(rune('a' + idx%26)),
				InputText:   string(rune('a' + idx%26)) + " world",
			}
			_ = comp.Pin(candidates, ctx)
		}(i)
	}
	wg.Wait()
	// If we get here without panic or race, the test passes.
}
