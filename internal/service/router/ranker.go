package router

import "github.com/gateyes/gateway/internal/service/provider"

func (r *Router) applyRankerLocked(candidates []provider.Provider, ctx RouteContext) []provider.Provider {
	if !r.cfg.Ranker.Enabled || len(candidates) == 0 {
		return candidates
	}

	switch r.cfg.Ranker.Method {
	case "", "none":
		return candidates
	case "ml_rank":
		// TODO(io-wy): add LightGBM/BERT based ranking once features, labels and online feedback are defined.
		return candidates
	default:
		return candidates
	}
}
