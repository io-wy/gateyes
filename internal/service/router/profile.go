package router

import "strings"

const (
	RoutingProfileLatency    = "latency"
	RoutingProfileCost       = "cost"
	RoutingProfileThroughput = "throughput"
	RoutingProfileSticky     = "sticky"
	RoutingProfileCache      = "cache"
	RoutingProfileRandom     = "random"
	RoutingProfileBalanced   = "balanced"
)

// NormalizeRoutingProfile maps public request-level profile names to the
// internal router strategies they should use. Unknown values fail open by
// returning empty strings, which preserves the configured default strategy.
func NormalizeRoutingProfile(raw string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "default":
		return "", ""
	case "latency", "load", "least_load", "least-load":
		return RoutingProfileLatency, "least_load"
	case "cost", "cheap", "cheapest", "cost_based", "cost-based":
		return RoutingProfileCost, "cost_based"
	case "throughput", "tpm", "least_tpm", "least-tpm":
		return RoutingProfileThroughput, "least_tpm"
	case "sticky", "session", "session_affinity", "session-affinity":
		return RoutingProfileSticky, "sticky"
	case "cache", "cache_affinity", "cache-affinity", "prefix", "prefix_affinity", "prefix-affinity":
		return RoutingProfileCache, ""
	case "random":
		return RoutingProfileRandom, "random"
	case "balanced", "round_robin", "round-robin":
		return RoutingProfileBalanced, "round_robin"
	default:
		return "", ""
	}
}

func ResolveRouteStrategy(defaultStrategy string, ctx RouteContext) string {
	if ctx.StrategyOverride != "" {
		return ctx.StrategyOverride
	}
	_, strategy := NormalizeRoutingProfile(ctx.RoutingProfile)
	if strategy != "" {
		return strategy
	}
	return defaultStrategy
}
