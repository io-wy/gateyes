package router

import (
	"regexp"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/service/provider"
)

func (r *Router) applyRuleEngineLocked(candidates []provider.Provider, ctx RouteContext) []provider.Provider {
	if !r.cfg.RuleEngine.Enabled || len(r.cfg.RuleEngine.Rules) == 0 || len(candidates) == 0 {
		return candidates
	}

	for _, rule := range r.cfg.RuleEngine.Rules {
		if !matchRouteRule(rule.Match, ctx) {
			continue
		}
		filtered := filterProvidersByName(candidates, rule.Action.Providers)
		if len(filtered) > 0 {
			return filtered
		}
		return candidates
	}

	return candidates
}

func matchRouteRule(match config.RouteMatchConfig, ctx RouteContext) bool {
	if len(match.Models) > 0 && !containsString(match.Models, ctx.Model) {
		return false
	}
	if match.MinPromptTokens > 0 && ctx.PromptTokens < match.MinPromptTokens {
		return false
	}
	if match.MaxPromptTokens > 0 && ctx.PromptTokens > match.MaxPromptTokens {
		return false
	}
	if match.HasTools != nil && ctx.HasTools != *match.HasTools {
		return false
	}
	if match.HasImages != nil && ctx.HasImages != *match.HasImages {
		return false
	}
	if match.HasStructuredOutput != nil && ctx.HasStructuredOutput != *match.HasStructuredOutput {
		return false
	}
	if match.Stream != nil && ctx.Stream != *match.Stream {
		return false
	}
	if len(match.AnyRegex) > 0 && !matchAnyRegex(match.AnyRegex, ctx.InputText) {
		return false
	}
	return true
}

func matchAnyRegex(patterns []string, input string) bool {
	if input == "" {
		return false
	}
	for _, pattern := range patterns {
		matched, err := regexp.MatchString(pattern, input)
		if err == nil && matched {
			return true
		}
	}
	return false
}

func filterProvidersByName(candidates []provider.Provider, names []string) []provider.Provider {
	if len(names) == 0 {
		return nil
	}
	filtered := make([]provider.Provider, 0, len(candidates))
	for _, candidate := range candidates {
		if containsString(names, candidate.Name()) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
