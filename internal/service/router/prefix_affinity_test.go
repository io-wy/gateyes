package router

import (
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/service/provider"
)

func TestPrefixAffinity_PinBeforePromote(t *testing.T) {
	pa := NewPrefixAffinity(10, 0)
	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
	}
	ordered := pa.Pin(candidates, RouteContext{InputText: "hello world"})
	if ordered[0].Name() != "p1" {
		t.Fatalf("before promote order should be unchanged")
	}
}

func TestPrefixAffinity_PinAfterPromote(t *testing.T) {
	pa := NewPrefixAffinity(10, 0)
	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
	}
	ctx := RouteContext{InputText: "hello world"}
	pa.Promote(ctx, "p2")
	ordered := pa.Pin(candidates, ctx)
	if ordered[0].Name() != "p2" {
		t.Fatalf("expected p2 to be pinned, got %s", ordered[0].Name())
	}
}

func TestPrefixAffinity_SamePrefixDifferentSuffix(t *testing.T) {
	pa := NewPrefixAffinity(5, 0) // first 5 runes
	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
	}
	pa.Promote(RouteContext{InputText: "hello world"}, "p2")
	ordered := pa.Pin(candidates, RouteContext{InputText: "hello there"})
	if ordered[0].Name() != "p2" {
		t.Fatalf("same prefix should pin to same provider")
	}
}

func TestPrefixAffinity_DifferentPrefix(t *testing.T) {
	pa := NewPrefixAffinity(10, 0)
	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
	}
	pa.Promote(RouteContext{InputText: "hello world"}, "p2")
	ordered := pa.Pin(candidates, RouteContext{InputText: "goodbye world"})
	if ordered[0].Name() != "p1" {
		t.Fatalf("different prefix should not be pinned")
	}
}

func TestPrefixAffinity_TTLExpiry(t *testing.T) {
	pa := NewPrefixAffinity(10, 1*time.Millisecond)
	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
		&mockProvider{name: "p2"},
	}
	ctx := RouteContext{InputText: "hello world"}
	pa.Promote(ctx, "p2")
	time.Sleep(5 * time.Millisecond)
	ordered := pa.Pin(candidates, ctx)
	if ordered[0].Name() != "p1" {
		t.Fatalf("expired prefix should not be pinned")
	}
}

func TestPrefixAffinity_EmptyInput(t *testing.T) {
	pa := NewPrefixAffinity(10, 0)
	candidates := []provider.Provider{
		&mockProvider{name: "p1"},
	}
	ordered := pa.Pin(candidates, RouteContext{InputText: ""})
	if len(ordered) != 1 || ordered[0].Name() != "p1" {
		t.Fatalf("empty input should not change order")
	}
}

func TestPrefixAffinity_PinSingleCandidate(t *testing.T) {
	pa := NewPrefixAffinity(10, 0)
	candidates := []provider.Provider{&mockProvider{name: "p1"}}
	pa.Promote(RouteContext{InputText: "hello"}, "p1")
	ordered := pa.Pin(candidates, RouteContext{InputText: "hello"})
	if len(ordered) != 1 {
		t.Fatalf("single candidate should be unchanged")
	}
}
