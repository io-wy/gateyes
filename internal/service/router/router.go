package router

import (
	"math/rand"
	"sort"
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
	return r.SelectFromWithContext(r.providers, RouteContext{
		Model:     model,
		SessionID: sessionID,
	})
}

func (r *Router) SelectFrom(candidates []provider.Provider, sessionID string) provider.Provider {
	return r.SelectFromWithContext(candidates, RouteContext{SessionID: sessionID})
}

func (r *Router) SelectFromWithModel(candidates []provider.Provider, sessionID string, model string) provider.Provider {
	return r.SelectFromWithContext(candidates, RouteContext{
		Model:     model,
		SessionID: sessionID,
	})
}

func (r *Router) SelectFromWithContext(candidates []provider.Provider, ctx RouteContext) provider.Provider {
	ordered := r.OrderCandidates(candidates, ctx)
	if len(ordered) == 0 {
		return nil
	}
	if ctx.Model != "" {
		for _, candidate := range ordered {
			if candidate.Model() == ctx.Model {
				return candidate
			}
		}
	}
	return ordered[0]
}

func (r *Router) OrderCandidates(candidates []provider.Provider, ctx RouteContext) []provider.Provider {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(candidates) == 0 {
		return nil
	}

	ordered := make([]provider.Provider, len(candidates))
	copy(ordered, candidates)

	ordered = r.applyRuleEngineLocked(ordered, ctx)
	ordered = r.applyRankerLocked(ordered, ctx)
	ordered = r.orderByStrategyLocked(ordered, ctx.SessionID)
	if len(ordered) == 0 {
		return nil
	}
	return ordered
}

func (r *Router) orderByStrategyLocked(candidates []provider.Provider, sessionID string) []provider.Provider {
	if len(candidates) <= 1 {
		return candidates
	}

	ordered := make([]provider.Provider, len(candidates))
	copy(ordered, candidates)

	switch r.cfg.Strategy {
	case "round_robin":
		start := r.index % len(ordered)
		result := append([]provider.Provider(nil), ordered[start:]...)
		result = append(result, ordered[:start]...)
		r.index = (r.index + 1) % len(ordered)
		return result
	case "least_load":
		sort.SliceStable(ordered, func(i, j int) bool {
			loadI := r.loads[ordered[i].Name()]
			loadJ := r.loads[ordered[j].Name()]
			return loadI < loadJ
		})
		return ordered
	case "cost_based":
		sort.SliceStable(ordered, func(i, j int) bool {
			return ordered[i].UnitCost() < ordered[j].UnitCost()
		})
		return ordered
	case "sticky":
		if sessionID == "" {
			start := r.index % len(ordered)
			result := append([]provider.Provider(nil), ordered[start:]...)
			result = append(result, ordered[:start]...)
			r.index = (r.index + 1) % len(ordered)
			return result
		}
		hash := 0
		for _, ch := range sessionID {
			hash = hash*31 + int(ch)
		}
		start := hash % len(ordered)
		if start < 0 {
			start = -start
		}
		result := append([]provider.Provider(nil), ordered[start:]...)
		result = append(result, ordered[:start]...)
		return result
	case "random":
		rand.Shuffle(len(ordered), func(i, j int) {
			ordered[i], ordered[j] = ordered[j], ordered[i]
		})
		return ordered
	case "ml_rank":
		// TODO(io-wy): once ranker is implemented, let strategy delegate to it or remove this alias.
		return ordered
	default:
		return ordered
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

func (r *Router) Strategy() string {
	return r.cfg.Strategy
}

func (r *Router) Load(name string) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loads[name]
}
