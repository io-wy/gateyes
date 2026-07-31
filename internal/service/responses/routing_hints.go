package responses

import (
	"context"

	routeSvc "github.com/gateyes/gateway/internal/service/router"
)

const RoutingProfileHeader = "X-Gateyes-Routing-Profile"

type RoutingHints struct {
	Profile          string
	StrategyOverride string
}

type routingHintsKey struct{}

func WithRoutingHints(ctx context.Context, hints RoutingHints) context.Context {
	if hints == (RoutingHints{}) {
		return ctx
	}
	return context.WithValue(ctx, routingHintsKey{}, hints)
}

func RoutingHintsFrom(ctx context.Context) RoutingHints {
	if ctx == nil {
		return RoutingHints{}
	}
	if h, ok := ctx.Value(routingHintsKey{}).(RoutingHints); ok {
		return h
	}
	return RoutingHints{}
}

func ParseRoutingHintsFromHeaders(header func(string) string) RoutingHints {
	profile, strategy := routeSvc.NormalizeRoutingProfile(header(RoutingProfileHeader))
	if profile == "" {
		// Compatibility alias for clients experimenting with AIBrix-style names.
		profile, strategy = routeSvc.NormalizeRoutingProfile(header("X-AIBrix-Routing-Profile"))
	}
	return RoutingHints{Profile: profile, StrategyOverride: strategy}
}

func WithGatewayHintsFromHeaders(ctx context.Context, header func(string) string) context.Context {
	ctx = WithCacheHints(ctx, ParseCacheHintsFromHeaders(header))
	ctx = WithRoutingHints(ctx, ParseRoutingHintsFromHeaders(header))
	return ctx
}

func applyRoutingHints(ctx context.Context, routeCtx routeSvc.RouteContext) routeSvc.RouteContext {
	hints := RoutingHintsFrom(ctx)
	profile, strategy := routeSvc.NormalizeRoutingProfile(hints.Profile)
	routeCtx.RoutingProfile = profile
	if hints.StrategyOverride != "" {
		routeCtx.StrategyOverride = hints.StrategyOverride
	} else {
		routeCtx.StrategyOverride = strategy
	}
	return routeCtx
}
