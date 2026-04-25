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
	stats     *provider.Stats
	index     int
	rrWeights map[string]int
	mu        sync.Mutex
}

func NewRouter(cfg config.RouterConfig, stats *provider.Stats) *Router {
	return &Router{
		cfg:       cfg,
		stats:     stats,
		rrWeights: make(map[string]int),
	}
}

func (r *Router) SetProviders(providers []provider.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers = providers
	valid := make(map[string]struct{}, len(providers))
	for _, p := range providers {
		valid[p.Name()] = struct{}{}
	}
	for name := range r.rrWeights {
		if _, ok := valid[name]; !ok {
			delete(r.rrWeights, name)
		}
	}
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

func (r *Router) ExplainOrderCandidates(candidates []provider.Provider, ctx RouteContext) ([]provider.Provider, OrderTrace) {
	r.mu.Lock()
	defer r.mu.Unlock()

	trace := OrderTrace{
		Initial:  providerNameList(candidates),
		Ranker:   r.cfg.Ranker.Method,
		Strategy: r.cfg.Strategy,
	}
	if len(candidates) == 0 {
		return nil, trace
	}

	ordered := make([]provider.Provider, len(candidates))
	copy(ordered, candidates)

	ordered, trace.Rule = r.applyRuleEngineTraceLocked(ordered, ctx)
	trace.AfterRule = providerNameList(ordered)
	ordered = r.applyRankerLocked(ordered, ctx)
	trace.AfterRanker = providerNameList(ordered)
	ordered = r.orderByStrategyLocked(ordered, ctx.SessionID)
	trace.Ordered = providerNameList(ordered)
	if len(ordered) == 0 {
		return nil, trace
	}
	return ordered, trace
}

func (r *Router) orderByStrategyLocked(candidates []provider.Provider, sessionID string) []provider.Provider {
	if len(candidates) <= 1 {
		return candidates
	}

	ordered := make([]provider.Provider, len(candidates))
	copy(ordered, candidates)

	switch r.cfg.Strategy {
	case "round_robin":
		return r.weightedRoundRobin(ordered)
	case "least_load":
		sort.SliceStable(ordered, func(i, j int) bool {
			var loadI, loadJ int64
			if r.stats != nil {
				loadI = r.stats.CurrentLoad(ordered[i].Name())
				loadJ = r.stats.CurrentLoad(ordered[j].Name())
			}
			if loadI != loadJ {
				return loadI < loadJ
			}
			return ordered[i].Weight() > ordered[j].Weight()
		})
		return ordered
	case "least_tpm":
		sort.SliceStable(ordered, func(i, j int) bool {
			var tpmI, tpmJ int64
			if r.stats != nil {
				tpmI = r.stats.TPM(ordered[i].Name())
				tpmJ = r.stats.TPM(ordered[j].Name())
			}
			if tpmI != tpmJ {
				return tpmI < tpmJ
			}
			return ordered[i].Weight() > ordered[j].Weight()
		})
		return ordered
	case "cost_based":
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].UnitCost() != ordered[j].UnitCost() {
				return ordered[i].UnitCost() < ordered[j].UnitCost()
			}
			return ordered[i].Weight() > ordered[j].Weight()
		})
		return ordered
	case "sticky":
		if sessionID == "" {
			return r.weightedRoundRobin(ordered)
		}
		totalWeight := 0
		for _, p := range ordered {
			w := p.Weight()
			if w <= 0 {
				w = 1
			}
			totalWeight += w
		}
		if totalWeight > 0 {
			hash := 0
			for _, ch := range sessionID {
				hash = hash*31 + int(ch)
			}
			pick := hash % totalWeight
			if pick < 0 {
				pick = -pick
			}
			cum := 0
			for i, p := range ordered {
				w := p.Weight()
				if w <= 0 {
					w = 1
				}
				cum += w
				if pick < cum {
					result := append([]provider.Provider{p}, ordered[:i]...)
					result = append(result, ordered[i+1:]...)
					return result
				}
			}
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
		totalWeight := 0
		for _, p := range ordered {
			w := p.Weight()
			if w <= 0 {
				w = 1
			}
			totalWeight += w
		}
		if totalWeight > 0 {
			pick := rand.Intn(totalWeight)
			cum := 0
			for i, p := range ordered {
				w := p.Weight()
				if w <= 0 {
					w = 1
				}
				cum += w
				if pick < cum {
					result := append([]provider.Provider{p}, ordered[:i]...)
					result = append(result, ordered[i+1:]...)
					rand.Shuffle(len(result)-1, func(a, b int) {
						result[a+1], result[b+1] = result[b+1], result[a+1]
					})
					return result
				}
			}
		}
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

func (r *Router) weightedRoundRobin(candidates []provider.Provider) []provider.Provider {
	if len(candidates) <= 1 {
		return candidates
	}
	totalWeight := 0
	for _, p := range candidates {
		w := p.Weight()
		if w <= 0 {
			w = 1
		}
		totalWeight += w
	}
	maxIdx := 0
	maxVal := -1
	for i, p := range candidates {
		w := p.Weight()
		if w <= 0 {
			w = 1
		}
		r.rrWeights[p.Name()] += w
		if r.rrWeights[p.Name()] > maxVal {
			maxVal = r.rrWeights[p.Name()]
			maxIdx = i
		}
	}
	selected := candidates[maxIdx]
	r.rrWeights[selected.Name()] -= totalWeight
	result := append([]provider.Provider{selected}, candidates[:maxIdx]...)
	result = append(result, candidates[maxIdx+1:]...)
	return result
}

func (r *Router) Strategy() string {
	return r.cfg.Strategy
}
