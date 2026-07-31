package responses

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/service/provider"
	routeSvc "github.com/gateyes/gateway/internal/service/router"
)

func TestRoutingHintsContextRoundtrip(t *testing.T) {
	ctx := context.Background()
	if got := RoutingHintsFrom(ctx); got != (RoutingHints{}) {
		t.Fatalf("empty ctx routing hints = %+v, want zero", got)
	}

	hints := RoutingHints{Profile: routeSvc.RoutingProfileCost, StrategyOverride: "cost_based"}
	ctx2 := WithRoutingHints(ctx, hints)
	if got := RoutingHintsFrom(ctx2); got != hints {
		t.Fatalf("RoutingHintsFrom = %+v, want %+v", got, hints)
	}
}

func TestRoutingHintsZeroValueDoesNotMutateCtx(t *testing.T) {
	base := context.Background()
	derived := WithRoutingHints(base, RoutingHints{})
	if derived != base {
		t.Fatal("zero routing hints should return same context to avoid wrapping")
	}
}

func TestParseRoutingHintsFromHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    RoutingHints
	}{
		{"empty", nil, RoutingHints{}},
		{"cost", map[string]string{RoutingProfileHeader: "cost"}, RoutingHints{Profile: routeSvc.RoutingProfileCost, StrategyOverride: "cost_based"}},
		{"latency alias", map[string]string{RoutingProfileHeader: "least-load"}, RoutingHints{Profile: routeSvc.RoutingProfileLatency, StrategyOverride: "least_load"}},
		{"cache", map[string]string{RoutingProfileHeader: "cache"}, RoutingHints{Profile: routeSvc.RoutingProfileCache}},
		{"aibrix alias", map[string]string{"X-AIBrix-Routing-Profile": "session"}, RoutingHints{Profile: routeSvc.RoutingProfileSticky, StrategyOverride: "sticky"}},
		{"unknown", map[string]string{RoutingProfileHeader: "bogus"}, RoutingHints{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseRoutingHintsFromHeaders(func(name string) string { return tc.headers[name] })
			if got != tc.want {
				t.Fatalf("ParseRoutingHintsFromHeaders() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestBuildRouteContextAppliesRoutingHints(t *testing.T) {
	base := context.Background()
	ctx := WithRoutingHints(base, RoutingHints{Profile: routeSvc.RoutingProfileCost, StrategyOverride: "cost_based"})
	routeCtx := buildRouteContext(ctx, &provider.ResponseRequest{Model: "m1", Input: "hello"}, "s1")
	if routeCtx.RoutingProfile != routeSvc.RoutingProfileCost || routeCtx.StrategyOverride != "cost_based" {
		t.Fatalf("buildRouteContext() = %+v, want routing profile and strategy override", routeCtx)
	}
}
