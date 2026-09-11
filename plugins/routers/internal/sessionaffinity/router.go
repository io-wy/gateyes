package sessionaffinity

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"

	pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"
	"github.com/gateyes/gateway/plugins/routers/internal/ordering"
)

type Router struct {
	pluginv1.UnimplementedRouterPluginServer
}

func New() *Router { return &Router{} }

func (r *Router) OrderCandidates(ctx context.Context, req *pluginv1.OrderCandidatesRequest) (*pluginv1.OrderCandidatesResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil {
		return &pluginv1.OrderCandidatesResponse{}, nil
	}
	candidates := ordering.UniqueCandidates(req.Candidates)
	if len(candidates) == 0 || req.Context == nil || req.Context.SessionId == "" {
		return &pluginv1.OrderCandidatesResponse{OrderedNames: ordering.NamesWithFirst(candidates, "")}, nil
	}
	key := req.Context.Model + "\x00" + req.Context.SessionId
	selected := rendezvousSelect(key, candidates)
	return &pluginv1.OrderCandidatesResponse{OrderedNames: ordering.NamesWithFirst(candidates, selected)}, nil
}

func rendezvousSelect(key string, candidates []*pluginv1.Candidate) string {
	selected := ""
	best := math.Inf(1)
	for _, candidate := range candidates {
		digest := sha256.Sum256([]byte(key + "\x00" + candidate.Name))
		hash := binary.BigEndian.Uint64(digest[:8])
		uniform := (float64(hash>>11) + 1) / (float64(uint64(1)<<53) + 1)
		score := -math.Log(uniform) / ordering.EffectiveWeight(candidate)
		if score < best || (score == best && candidate.Name < selected) {
			best = score
			selected = candidate.Name
		}
	}
	return selected
}
