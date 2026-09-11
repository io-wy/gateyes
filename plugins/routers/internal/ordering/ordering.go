package ordering

import pluginv1 "github.com/gateyes/gateway/pkg/plugin/v1"

func UniqueCandidates(candidates []*pluginv1.Candidate) []*pluginv1.Candidate {
	seen := make(map[string]struct{}, len(candidates))
	unique := make([]*pluginv1.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.Name == "" {
			continue
		}
		if _, ok := seen[candidate.Name]; ok {
			continue
		}
		seen[candidate.Name] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
}

func NamesWithFirst(candidates []*pluginv1.Candidate, selected string) []string {
	unique := UniqueCandidates(candidates)
	names := make([]string, 0, len(unique))
	if selected != "" {
		for _, candidate := range unique {
			if candidate.Name == selected {
				names = append(names, selected)
				break
			}
		}
	}
	for _, candidate := range unique {
		if candidate.Name != selected {
			names = append(names, candidate.Name)
		}
	}
	return names
}

func EffectiveWeight(candidate *pluginv1.Candidate) float64 {
	if candidate == nil || candidate.Weight <= 0 {
		return 1
	}
	return float64(candidate.Weight)
}
