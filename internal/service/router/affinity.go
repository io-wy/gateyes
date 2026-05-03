package router

import (
	"github.com/gateyes/gateway/internal/service/provider"
)

// Affinity reorders candidates so that a previously-selected provider is
// preferred for subsequent matching requests. It is a "soft" pin: if the
// preferred provider is not in the current candidate list the order is
// unchanged.
//
// Affinity sits between the Ranker and the Strategy in the router pipeline:
//
//	RuleEngine(filter) → Ranker(rerank) → Affinity(pin) → Strategy(select)
type Affinity interface {
	// Pin moves the preferred provider (if any) to the front of candidates.
	// The slice must not be modified in-place; return a new slice.
	Pin(candidates []provider.Provider, ctx RouteContext) []provider.Provider

	// Promote updates internal book-keeping after a successful request.
	// Callers should invoke this when the upstream responds successfully.
	Promote(ctx RouteContext, providerName string)
}

// CompositeAffinity runs multiple affinities in order. The first affinity
// that produces a pin wins; subsequent affinities are skipped.
type CompositeAffinity struct {
	chain []Affinity
}

// NewCompositeAffinity builds a composite from the given affinities.
// Affinities are evaluated in the order provided.
func NewCompositeAffinity(chain ...Affinity) *CompositeAffinity {
	return &CompositeAffinity{chain: chain}
}

// Pin delegates to each affinity in order until one produces a non-trivial
// reordering (the first candidate changes).
func (c *CompositeAffinity) Pin(candidates []provider.Provider, ctx RouteContext) []provider.Provider {
	if len(c.chain) == 0 || len(candidates) <= 1 {
		return candidates
	}
	for _, a := range c.chain {
		pinned := a.Pin(candidates, ctx)
		if len(pinned) > 0 && pinned[0].Name() != candidates[0].Name() {
			return pinned
		}
	}
	return candidates
}

// Promote forwards to all child affinities.
func (c *CompositeAffinity) Promote(ctx RouteContext, providerName string) {
	for _, a := range c.chain {
		a.Promote(ctx, providerName)
	}
}

// NoopAffinity is an identity affinity that never pins.
var NoopAffinity Affinity = noopAffinity{}

type noopAffinity struct{}

func (noopAffinity) Pin(candidates []provider.Provider, _ RouteContext) []provider.Provider {
	return candidates
}
func (noopAffinity) Promote(_ RouteContext, _ string) {}
