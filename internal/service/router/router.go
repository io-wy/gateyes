package router

import (
	"math"
	"math/rand"
	"sync"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/service/provider"
)

type Router struct {
	cfg       config.RouterConfig
	providers []provider.Provider
	index     int
	loads     map[string]int64
	mu        sync.Mutex
}

func NewRouter(cfg config.RouterConfig) *Router {
	return &Router{
		cfg:   cfg,
		loads: make(map[string]int64),
	}
}

func (r *Router) SetProviders(providers []provider.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = providers
}

func (r *Router) Select(model, sessionID string) provider.Provider {
	return r.SelectFrom(r.providers, sessionID)
}

func (r *Router) SelectFrom(candidates []provider.Provider, sessionID string) provider.Provider {
	return r.selectFromWithModel(candidates, sessionID, "")
}

func (r *Router) SelectFromWithModel(candidates []provider.Provider, sessionID string, model string) provider.Provider {
	return r.selectFromWithModel(candidates, sessionID, model)
}

func (r *Router) selectFromWithModel(candidates []provider.Provider, sessionID string, model string) provider.Provider {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(candidates) == 0 {
		return nil
	}

	// 如果指定了模型，优先选择支持该模型的 provider
	if model != "" {
		for _, p := range candidates {
			if p.Model() == model {
				return p
			}
		}
	}

	switch r.cfg.Strategy {
	case "random":
		return candidates[rand.Intn(len(candidates))]
	case "least_load":
		return r.leastLoadLocked(candidates)
	case "cost_based":
		return r.costBasedLocked(candidates)
	case "sticky":
		return r.stickyLocked(sessionID, candidates)
	default:
		return r.roundRobinLocked(candidates)
	}
}

func (r *Router) IncLoad(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loads[name]++
}

func (r *Router) DecLoad(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loads[name] > 0 {
		r.loads[name]--
	}
}

func (r *Router) roundRobinLocked(candidates []provider.Provider) provider.Provider {
	selected := candidates[r.index%len(candidates)]
	r.index = (r.index + 1) % len(candidates)
	return selected
}

func (r *Router) leastLoadLocked(candidates []provider.Provider) provider.Provider {
	var selected provider.Provider
	minLoad := int64(math.MaxInt64)
	for _, p := range candidates {
		load := r.loads[p.Name()]
		if load < minLoad {
			minLoad = load
			selected = p
		}
	}
	return selected
}

func (r *Router) costBasedLocked(candidates []provider.Provider) provider.Provider {
	var selected provider.Provider
	minCost := math.MaxFloat64
	for _, p := range candidates {
		if p.UnitCost() < minCost {
			minCost = p.UnitCost()
			selected = p
		}
	}
	return selected
}

func (r *Router) stickyLocked(sessionID string, candidates []provider.Provider) provider.Provider {
	if sessionID == "" {
		return r.roundRobinLocked(candidates)
	}

	hash := 0
	for _, ch := range sessionID {
		hash = hash*31 + int(ch)
	}

	index := hash % len(candidates)
	if index < 0 {
		index = -index
	}
	return candidates[index]
}
