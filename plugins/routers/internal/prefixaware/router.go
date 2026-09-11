package prefixaware

import (
	"context"
	"math/rand/v2"

	pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"
	"github.com/gateyes/gateway/plugins/routers/internal/ordering"
)

type Config struct {
	PrefixMinMatchLength int
	ChunkSize            int
}

type Router struct {
	pluginv1.UnimplementedRouterPluginServer
	trie           *HashTrie
	qps            *qpsTracker
	minMatchLength int
}

func New(cfg Config) *Router {
	minMatchLength := cfg.PrefixMinMatchLength
	if minMatchLength < 0 {
		minMatchLength = 0
	}
	return &Router{trie: NewHashTrie(cfg.ChunkSize), qps: newQPSTracker(), minMatchLength: minMatchLength}
}

func (r *Router) OrderCandidates(ctx context.Context, req *pluginv1.OrderCandidatesRequest) (*pluginv1.OrderCandidatesResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil {
		return &pluginv1.OrderCandidatesResponse{}, nil
	}
	candidates := ordering.UniqueCandidates(req.Candidates)
	if len(candidates) == 0 {
		return &pluginv1.OrderCandidatesResponse{}, nil
	}
	names := make([]string, len(candidates))
	available := make(map[string]struct{}, len(candidates))
	for i, candidate := range candidates {
		names[i] = candidate.Name
		available[candidate.Name] = struct{}{}
	}
	prompt := ""
	if req.Context != nil {
		if req.Context.PrefixText != nil {
			prompt = req.Context.GetPrefixText()
		} else {
			prompt = req.Context.InputText
		}
	}
	matchLength, matched := r.trie.LongestPrefixMatch(prompt, available)
	selected := ""
	if matchLength < r.minMatchLength {
		selected = r.qps.selectLowest(names)
	} else if len(matched) > 0 {
		selected = matched[rand.IntN(len(matched))]
	}
	if selected == "" {
		selected = names[0]
	}
	r.trie.Insert(prompt, selected)
	r.qps.record(selected)
	return &pluginv1.OrderCandidatesResponse{OrderedNames: ordering.NamesWithFirst(candidates, selected)}, nil
}
