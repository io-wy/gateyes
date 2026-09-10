package leastload

import (
	"context"
	"sort"
	"time"

	pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"
	"github.com/gateyes/gateway/plugins/routers/internal/ordering"
)

const defaultSignalMaxAge = 15 * time.Second

type Config struct {
	SignalMaxAge time.Duration
}

type Router struct {
	pluginv1.UnimplementedRouterPluginServer
	signalMaxAge time.Duration
	now          func() time.Time
}

func New(cfg Config) *Router {
	maxAge := cfg.SignalMaxAge
	if maxAge <= 0 {
		maxAge = defaultSignalMaxAge
	}
	return &Router{signalMaxAge: maxAge, now: time.Now}
}

func (r *Router) OrderCandidates(ctx context.Context, req *pluginv1.OrderCandidatesRequest) (*pluginv1.OrderCandidatesResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil {
		return &pluginv1.OrderCandidatesResponse{}, nil
	}
	candidates := ordering.UniqueCandidates(req.Candidates)
	now := r.now()
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.Healthy != right.Healthy {
			return left.Healthy
		}
		leftScore := r.loadScore(left, now)
		rightScore := r.loadScore(right, now)
		if leftScore != rightScore {
			return leftScore < rightScore
		}
		if comparison := compareObservedMetric(left.AvgTtftMs, right.AvgTtftMs); comparison != 0 {
			return comparison < 0
		}
		if comparison := compareObservedMetric(left.AvgLatencyMs, right.AvgLatencyMs); comparison != 0 {
			return comparison < 0
		}
		return left.Name < right.Name
	})
	return &pluginv1.OrderCandidatesResponse{OrderedNames: ordering.NamesWithFirst(candidates, "")}, nil
}

func (r *Router) loadScore(candidate *pluginv1.Candidate, now time.Time) float64 {
	weight := ordering.EffectiveWeight(candidate)
	if r.hasFreshSignals(candidate, now) {
		return (candidate.QueueWaiting*4 + candidate.QueueRunning + candidate.GpuKvCacheUsagePerc*2 + candidate.CpuKvCacheUsagePerc) / weight
	}
	return float64(candidate.CurrentLoad) / weight
}

func (r *Router) hasFreshSignals(candidate *pluginv1.Candidate, now time.Time) bool {
	if candidate.SignalsUpdatedAtUnixMs <= 0 {
		return false
	}
	age := now.Sub(time.UnixMilli(candidate.SignalsUpdatedAtUnixMs))
	return age <= r.signalMaxAge
}

func compareObservedMetric(left, right float64) int {
	leftKnown, rightKnown := left > 0, right > 0
	if leftKnown != rightKnown {
		if leftKnown {
			return -1
		}
		return 1
	}
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
