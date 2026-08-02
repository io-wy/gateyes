package router

import (
	"regexp"
	"sync"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/service/provider"
)

var regexCache sync.Map // map[string]*regexp.Regexp

func cachedRegexp(pattern string) *regexp.Regexp {
	if re, ok := regexCache.Load(pattern); ok {
		return re.(*regexp.Regexp)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	regexCache.Store(pattern, re)
	return re
}

func (r *Router) applyRuleEngineLocked(candidates []provider.Provider, ctx RouteContext) []provider.Provider {
	filtered, _ := r.applyRuleEngineTraceLocked(candidates, ctx)
	return filtered
}

func (r *Router) applyRuleEngineTraceLocked(candidates []provider.Provider, ctx RouteContext) ([]provider.Provider, RuleTrace) {
	if !r.cfg.RuleEngine.Enabled || len(r.cfg.RuleEngine.Rules) == 0 || len(candidates) == 0 {
		return candidates, RuleTrace{}
	}

	for _, rule := range r.cfg.RuleEngine.Rules {
		if !matchRouteRule(rule.Match, ctx) {
			continue
		}
		filtered := filterProviders(candidates, rule.Action)
		if len(filtered) > 0 {
			return filtered, RuleTrace{
				Matched:   true,
				RuleName:  rule.Name,
				Providers: providerNameList(filtered),
			}
		}
		return candidates, RuleTrace{
			Matched:   true,
			RuleName:  rule.Name,
			Providers: providerNameList(candidates),
		}
	}

	return candidates, RuleTrace{}
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
		re := cachedRegexp(pattern)
		if re != nil && re.MatchString(input) {
			return true
		}
	}
	return false
}

func filterProviders(candidates []provider.Provider, action config.RouteActionConfig) []provider.Provider {
	filtered := candidates
	if len(action.Providers) > 0 {
		filtered = filterProvidersByName(filtered, action.Providers)
	}
	if len(action.ProviderLabels) > 0 {
		filtered = filterProvidersByLabels(filtered, action.ProviderLabels)
	}
	if len(action.Providers) == 0 && len(action.ProviderLabels) == 0 {
		return nil
	}
	return filtered
}

func filterProvidersByName(candidates []provider.Provider, names []string) []provider.Provider {
	filtered := make([]provider.Provider, 0, len(candidates))
	for _, candidate := range candidates {
		if containsString(names, candidate.Name()) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func filterProvidersByLabels(candidates []provider.Provider, labels map[string]string) []provider.Provider {
	filtered := make([]provider.Provider, 0, len(candidates))
	for _, candidate := range candidates {
		if providerLabelsMatch(candidate.Labels(), labels) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func providerLabelsMatch(providerLabels map[string]string, required map[string]string) bool {
	for key, value := range required {
		if providerLabels[key] != value {
			return false
		}
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
