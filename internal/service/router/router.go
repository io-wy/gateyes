package router

import (
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/service/provider"
)

type Router struct {
	cfg              config.RouterConfig
	providers        []provider.Provider
	stats            *provider.Stats
	index            int
	rrWeights        map[string]int
	mu               sync.Mutex
	affinity         Affinity
	profileSession   *SessionAffinity
	profilePrefix    *PrefixAffinity
	inferenceScraper *InferenceScraper
}

// SetInferenceScraper wires an optional inference-signal scraper. When
// set, the least_load strategy prefers its load score over in-process
// CurrentLoad. nil disables (default).
func (r *Router) SetInferenceScraper(s *InferenceScraper) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inferenceScraper = s
}

func NewRouter(cfg config.RouterConfig, stats *provider.Stats) *Router {
	r := &Router{
		cfg:       cfg,
		stats:     stats,
		rrWeights: make(map[string]int),
		affinity:  NoopAffinity,
	}
	r.initProfileAffinities()
	r.initAffinity()
	return r
}

func (r *Router) initProfileAffinities() {
	sessionTTL := time.Duration(r.cfg.Affinity.SessionTTL) * time.Second
	prefixTTL := time.Duration(r.cfg.Affinity.PrefixTTL) * time.Second
	prefixDepth := r.cfg.Affinity.PrefixDepth
	if prefixDepth < 0 {
		prefixDepth = 0
	}
	r.profileSession = NewSessionAffinity(sessionTTL)
	r.profilePrefix = NewPrefixAffinity(prefixDepth, prefixTTL)
}

func (r *Router) initAffinity() {
	if !r.cfg.Affinity.Enabled && r.cfg.Strategy != "sticky" {
		return
	}
	var chain []Affinity
	if r.cfg.Strategy == "sticky" || r.cfg.Affinity.Enabled {
		// Backward compat: legacy "sticky" strategy auto-enables session affinity.
		ttl := time.Duration(r.cfg.Affinity.SessionTTL) * time.Second
		chain = append(chain, NewSessionAffinity(ttl))
	}
	if r.cfg.Affinity.Enabled && r.cfg.Affinity.PrefixDepth >= 0 {
		ttl := time.Duration(r.cfg.Affinity.PrefixTTL) * time.Second
		chain = append(chain, NewPrefixAffinity(r.cfg.Affinity.PrefixDepth, ttl))
	}
	if len(chain) > 0 {
		r.affinity = NewCompositeAffinity(chain...)
	}
}

func (r *Router) SetAffinity(a Affinity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.affinity = a
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
	beforeAffinity := ordered
	ordered = r.applyAffinityLocked(ordered, ctx)
	ordered = r.orderByStrategyLocked(ordered, r.strategyAfterAffinity(ctx, beforeAffinity, ordered))
	if len(ordered) == 0 {
		return nil
	}
	return ordered
}

func (r *Router) ExplainOrderCandidates(candidates []provider.Provider, ctx RouteContext) ([]provider.Provider, OrderTrace) {
	r.mu.Lock()
	defer r.mu.Unlock()

	routingProfile, _ := NormalizeRoutingProfile(ctx.RoutingProfile)
	trace := OrderTrace{
		Initial:        providerNameList(candidates),
		Ranker:         r.cfg.Ranker.Method,
		Affinity:       r.affinityName(ctx),
		RoutingProfile: routingProfile,
		Strategy:       ResolveRouteStrategy(r.cfg.Strategy, ctx),
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
	beforeAffinity := ordered
	ordered = r.applyAffinityLocked(ordered, ctx)
	trace.AfterAffinity = providerNameList(ordered)
	trace.Strategy = r.strategyAfterAffinity(ctx, beforeAffinity, ordered)
	ordered = r.orderByStrategyLocked(ordered, trace.Strategy)
	trace.Ordered = providerNameList(ordered)
	trace.Scores = r.scoreTraceLocked(ordered, trace.Strategy)
	if len(ordered) == 0 {
		return nil, trace
	}
	return ordered, trace
}

func (r *Router) orderByStrategyLocked(candidates []provider.Provider, strategy string) []provider.Provider {
	if len(candidates) <= 1 {
		return candidates
	}

	ordered := make([]provider.Provider, len(candidates))
	copy(ordered, candidates)

	switch strategy {
	case "round_robin":
		return r.weightedRoundRobin(ordered)
	case "least_load":
		sort.SliceStable(ordered, func(i, j int) bool {
			scoreI, hasI := r.inferenceLoadLocked(ordered[i].Name())
			scoreJ, hasJ := r.inferenceLoadLocked(ordered[j].Name())
			if hasI && hasJ && scoreI != scoreJ {
				return scoreI < scoreJ
			}
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
	case "least_latency":
		sort.SliceStable(ordered, func(i, j int) bool {
			latI, hasI := r.avgLatencyLocked(ordered[i].Name())
			latJ, hasJ := r.avgLatencyLocked(ordered[j].Name())
			if hasI && hasJ && latI != latJ {
				return latI < latJ
			}
			if hasI != hasJ {
				return hasI
			}
			return ordered[i].Weight() > ordered[j].Weight()
		})
		return ordered
	case "least_kv_cache":
		sort.SliceStable(ordered, func(i, j int) bool {
			cacheI, hasI := r.kvCacheUsageLocked(ordered[i].Name())
			cacheJ, hasJ := r.kvCacheUsageLocked(ordered[j].Name())
			if hasI && hasJ && cacheI != cacheJ {
				return cacheI < cacheJ
			}
			if hasI != hasJ {
				return hasI
			}
			return ordered[i].Weight() > ordered[j].Weight()
		})
		return ordered
	case "least_gpu_cache":
		sort.SliceStable(ordered, func(i, j int) bool {
			cacheI, hasI := r.gpuCacheUsageLocked(ordered[i].Name())
			cacheJ, hasJ := r.gpuCacheUsageLocked(ordered[j].Name())
			if hasI && hasJ && cacheI != cacheJ {
				return cacheI < cacheJ
			}
			if hasI != hasJ {
				return hasI
			}
			return ordered[i].Weight() > ordered[j].Weight()
		})
		return ordered
	case "power_of_two":
		return r.powerOfTwoChoices(ordered)
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
		// sticky has been migrated to the Affinity layer (SessionAffinity).
		// Keeping this case for config backward compatibility; the actual
		// pinning happens in applyAffinityLocked before strategy runs.
		return ordered
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
	default:
		return ordered
	}
}

func (r *Router) strategyAfterAffinity(ctx RouteContext, before, after []provider.Provider) string {
	strategy := ResolveRouteStrategy(r.cfg.Strategy, ctx)
	profile, _ := NormalizeRoutingProfile(ctx.RoutingProfile)
	if profile == RoutingProfileCache && firstCandidateChanged(before, after) {
		return "sticky"
	}
	return strategy
}

func firstCandidateChanged(before, after []provider.Provider) bool {
	if len(before) == 0 || len(after) == 0 {
		return false
	}
	return before[0].Name() != after[0].Name()
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

func (r *Router) powerOfTwoChoices(candidates []provider.Provider) []provider.Provider {
	if len(candidates) <= 2 {
		return r.orderByStrategyLocked(candidates, "least_load")
	}
	i := rand.Intn(len(candidates))
	j := rand.Intn(len(candidates) - 1)
	if j >= i {
		j++
	}
	pick, other := candidates[i], candidates[j]
	scorePick := r.comparableLoadLocked(pick.Name())
	scoreOther := r.comparableLoadLocked(other.Name())
	if scoreOther < scorePick {
		pick = other
	}
	result := make([]provider.Provider, 0, len(candidates))
	result = append(result, pick)
	for idx, candidate := range candidates {
		if idx == i || idx == j {
			continue
		}
		result = append(result, candidate)
	}
	if pick.Name() == candidates[i].Name() {
		result = append(result, candidates[j])
	} else {
		result = append(result, candidates[i])
	}
	return result
}

func (r *Router) applyAffinityLocked(candidates []provider.Provider, ctx RouteContext) []provider.Provider {
	if len(candidates) <= 1 {
		return candidates
	}
	ordered := candidates
	if r.affinity != nil {
		ordered = r.affinity.Pin(candidates, ctx)
	}
	switch profile, _ := NormalizeRoutingProfile(ctx.RoutingProfile); profile {
	case RoutingProfileSticky:
		if r.profileSession != nil {
			ordered = r.profileSession.Pin(ordered, ctx)
		}
	case RoutingProfileCache:
		if r.profilePrefix != nil {
			ordered = r.profilePrefix.Pin(ordered, ctx)
		}
	}
	return ordered
}

func (r *Router) affinityName(ctx RouteContext) string {
	base := "custom"
	switch r.affinity.(type) {
	case *CompositeAffinity:
		base = "composite"
	case *SessionAffinity:
		base = "session"
	case *PrefixAffinity:
		base = "prefix"
	case noopAffinity:
		base = "none"
	}
	switch profile, _ := NormalizeRoutingProfile(ctx.RoutingProfile); profile {
	case RoutingProfileSticky:
		return base + "+profile:sticky"
	case RoutingProfileCache:
		return base + "+profile:cache"
	}
	return base
}

// inferenceLoadLocked returns the synthetic load score for a provider as
// reported by the inference scraper. Boolean is false when no scraper is
// configured or no fresh state exists for that provider.
func (r *Router) inferenceLoadLocked(name string) (float64, bool) {
	if r.inferenceScraper == nil {
		return 0, false
	}
	state, ok := r.inferenceScraper.Get(name)
	if !ok || state.Stale {
		return 0, false
	}
	return state.LoadScore(), true
}

func (r *Router) comparableLoadLocked(name string) float64 {
	if score, ok := r.inferenceLoadLocked(name); ok {
		return score
	}
	if r.stats == nil {
		return 0
	}
	return float64(r.stats.CurrentLoad(name))
}

func (r *Router) avgLatencyLocked(name string) (float64, bool) {
	if r.stats == nil {
		return 0, false
	}
	stats, ok := r.stats.Get(name)
	if !ok || stats.AvgLatencyMs <= 0 {
		return 0, false
	}
	return stats.AvgLatencyMs, true
}

func (r *Router) kvCacheUsageLocked(name string) (float64, bool) {
	if r.inferenceScraper == nil {
		return 0, false
	}
	state, ok := r.inferenceScraper.Get(name)
	if !ok || state.Stale {
		return 0, false
	}
	return state.GPUCacheUsagePerc + state.CPUCacheUsagePerc, true
}

func (r *Router) gpuCacheUsageLocked(name string) (float64, bool) {
	if r.inferenceScraper == nil {
		return 0, false
	}
	state, ok := r.inferenceScraper.Get(name)
	if !ok || state.Stale {
		return 0, false
	}
	return state.GPUCacheUsagePerc, true
}

func (r *Router) scoreTraceLocked(candidates []provider.Provider, strategy string) []ScoreTrace {
	if len(candidates) == 0 {
		return nil
	}
	scores := make([]ScoreTrace, 0, len(candidates))
	for _, candidate := range candidates {
		score := ScoreTrace{
			Provider:      candidate.Name(),
			LowerIsBetter: true,
			Components:    map[string]float64{},
		}
		switch strategy {
		case "least_load", "power_of_two":
			if inferenceLoad, ok := r.inferenceLoadLocked(candidate.Name()); ok {
				score.Components["inference_load"] = inferenceLoad
				score.Total = inferenceLoad
			} else if r.stats != nil {
				load := float64(r.stats.CurrentLoad(candidate.Name()))
				score.Components["current_load"] = load
				score.Total = load
			}
		case "least_latency":
			if latency, ok := r.avgLatencyLocked(candidate.Name()); ok {
				score.Components["avg_latency_ms"] = latency
				score.Total = latency
			}
		case "least_tpm":
			if r.stats != nil {
				tpm := float64(r.stats.TPM(candidate.Name()))
				score.Components["tpm"] = tpm
				score.Total = tpm
			}
		case "cost_based":
			cost := candidate.UnitCost()
			score.Components["unit_cost"] = cost
			score.Total = cost
		case "least_kv_cache":
			if state, ok := r.inferenceStateLocked(candidate.Name()); ok {
				score.Components["gpu_cache_usage"] = state.GPUCacheUsagePerc
				score.Components["cpu_cache_usage"] = state.CPUCacheUsagePerc
				score.Total = state.GPUCacheUsagePerc + state.CPUCacheUsagePerc
			}
		case "least_gpu_cache":
			if state, ok := r.inferenceStateLocked(candidate.Name()); ok {
				score.Components["gpu_cache_usage"] = state.GPUCacheUsagePerc
				score.Total = state.GPUCacheUsagePerc
			}
		default:
			weight := float64(candidate.Weight())
			score.LowerIsBetter = false
			score.Components["weight"] = weight
			score.Total = weight
		}
		if len(score.Components) == 0 {
			score.Components = nil
		}
		scores = append(scores, score)
	}
	return scores
}

func (r *Router) inferenceStateLocked(name string) (InferenceState, bool) {
	if r.inferenceScraper == nil {
		return InferenceState{}, false
	}
	state, ok := r.inferenceScraper.Get(name)
	if !ok || state.Stale {
		return InferenceState{}, false
	}
	return state, true
}

func (r *Router) PromoteAffinity(ctx RouteContext, providerName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.affinity != nil {
		r.affinity.Promote(ctx, providerName)
	}
	switch profile, _ := NormalizeRoutingProfile(ctx.RoutingProfile); profile {
	case RoutingProfileSticky:
		if r.profileSession != nil {
			r.profileSession.Promote(ctx, providerName)
		}
	case RoutingProfileCache:
		if r.profilePrefix != nil {
			r.profilePrefix.Promote(ctx, providerName)
		}
	}
}

func (r *Router) Strategy() string {
	return r.cfg.Strategy
}

// Reload updates runtime-safe router parameters.
func (r *Router) Reload(cfg *config.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg = cfg.Router
	r.affinity = NoopAffinity
	r.initProfileAffinities()
	r.initAffinity()
	return nil
}

func (r *Router) Name() string { return "router" }
