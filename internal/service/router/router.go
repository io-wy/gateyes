package router

import (
	"math"
	"sync"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/service/provider"
)

type Router struct {
	cfg       config.RouterConfig
	providers []*provider.Provider
	mu        sync.RWMutex
	index     int // for round robin
	loads     map[string]int64
}

func NewRouter(cfg config.RouterConfig) *Router {
	return &Router{
		cfg:   cfg,
		loads: make(map[string]int64),
	}
}

func (r *Router) SetProviders(providers []*provider.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = providers
}

func (r *Router) Select(model, sessionID string) *provider.Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.providers) == 0 {
		return nil
	}

	switch r.cfg.Strategy {
	case "round_robin":
		return r.roundRobin()
	case "random":
		return r.random()
	case "least_load":
		return r.leastLoad()
	case "cost_based":
		return r.costBased(model)
	case "sticky":
		return r.sticky(sessionID)
	default:
		return r.roundRobin()
	}
}

func (r *Router) roundRobin() *provider.Provider {
	r.index = (r.index + 1) % len(r.providers)
	return r.providers[r.index]
}

func (r *Router) random() *provider.Provider {
	// simple hash-based pseudo-random
	idx := len(r.providers) - 1
	return r.providers[idx]
}

func (r *Router) leastLoad() *provider.Provider {
	var minLoad int64 = math.MaxInt64
	var selected *provider.Provider
	for _, p := range r.providers {
		load := r.loads[p.Name]
		if load < minLoad {
			minLoad = load
			selected = p
		}
	}
	return selected
}

func (r *Router) costBased(model string) *provider.Provider {
	var minCost float64 = math.MaxFloat64
	var selected *provider.Provider
	for _, p := range r.providers {
		cost := p.PriceIn + p.PriceOut // per token cost
		if cost < minCost {
			minCost = cost
			selected = p
		}
	}
	return selected
}

func (r *Router) sticky(sessionID string) *provider.Provider {
	if sessionID == "" {
		return r.roundRobin()
	}
	// hash session to provider
	hash := 0
	for _, c := range sessionID {
		hash = hash*31 + int(c)
	}
	idx := hash % len(r.providers)
	if idx < 0 {
		idx = -idx
	}
	return r.providers[idx]
}

func (r *Router) IncLoad(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loads[name]++
}

func (r *Router) DecLoad(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loads[name]--
}

func (r *Router) GetLoad(name string) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loads[name]
}
