package ingress

import (
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/gateyes/gateway/internal/proxy"
)

// RouteTable is a thread-safe lookup table for ingress routes.
// Matching priority: Exact > Prefix > wildcard host.
type RouteTable struct {
	rules []proxy.RouteRule
	mu    sync.RWMutex
}

func NewRouteTable() *RouteTable {
	return &RouteTable{}
}

// Replace atomically replaces all routes.
func (rt *RouteTable) Replace(rules []proxy.RouteRule) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	// Sort by specificity for deterministic matching.
	sortRoutes(rules)
	rt.rules = rules
}

// Lookup finds the best matching route for a request.
func (rt *RouteTable) Lookup(req *http.Request) *proxy.RouteRule {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	var best *proxy.RouteRule
	bestScore := -1

	for i := range rt.rules {
		r := &rt.rules[i]
		if !r.Match(req) {
			continue
		}
		score := routeScore(r, req)
		if score > bestScore {
			bestScore = score
			best = r
		}
	}
	return best
}

// List returns a snapshot of all routes.
func (rt *RouteTable) List() []proxy.RouteRule {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	out := make([]proxy.RouteRule, len(rt.rules))
	copy(out, rt.rules)
	return out
}

// routeScore computes a match specificity score. Higher = more specific.
func routeScore(r *proxy.RouteRule, req *http.Request) int {
	score := 0
	if r.Host != "" {
		score += 100
		if !strings.HasPrefix(r.Host, "*.") {
			score += 50 // exact host beats wildcard
		}
	}
	switch r.PathType {
	case proxy.PathTypeExact:
		score += 30 + len(r.Path)
	case proxy.PathTypePrefix:
		score += 10 + len(r.Path)
	default:
		score += len(r.Path)
	}
	return score
}

func sortRoutes(rules []proxy.RouteRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		// More specific routes first.
		return routeScore(&rules[i], nil) > routeScore(&rules[j], nil)
	})
}
