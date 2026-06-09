package grpc

import (
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/gateyes/gateway/internal/domain/plugin"
)

func TestGetByTypeAndPhaseRequireHealthyPlugin(t *testing.T) {
	m := &Manager{clients: map[string]*clientWrapper{
		"router": {
			name:       "router",
			pluginType: "router",
			health:     plugin.HealthHealthy,
		},
		"gateway-healthy": {
			name:       "gateway-healthy",
			pluginType: "gateway",
			phases:     []string{string(plugin.PreUpstream)},
			health:     plugin.HealthHealthy,
		},
		"gateway-unhealthy": {
			name:       "gateway-unhealthy",
			pluginType: "gateway",
			phases:     []string{string(plugin.PreUpstream)},
			health:     plugin.HealthUnhealthy,
		},
	}}

	if got := m.GetByType("router"); got == nil || got.name != "router" {
		t.Fatalf("GetByType(router) = %+v, want healthy router", got)
	}
	if got := m.GetByType("gateway"); got == nil || got.name != "gateway-healthy" {
		t.Fatalf("GetByType(gateway) = %+v, want healthy gateway", got)
	}

	plugins := m.GetByPhase(plugin.PreUpstream)
	if len(plugins) != 1 || plugins[0].Name() != "gateway-healthy" {
		t.Fatalf("GetByPhase(pre_upstream) = %+v, want healthy gateway only", plugins)
	}
	if plugins := m.GetByPhase(plugin.PostUpstream); len(plugins) != 0 {
		t.Fatalf("GetByPhase(post_upstream) len = %d, want 0", len(plugins))
	}
}

func TestPluginClientsRespectHealthGate(t *testing.T) {
	router := &routerPluginClient{wrapper: &clientWrapper{health: plugin.HealthUnhealthy}}
	if ordered, ok := router.OrderCandidates(nil, nil, plugin.RouteContext{}); ok || ordered != nil {
		t.Fatalf("OrderCandidates(unhealthy) = (%v,%v), want nil,false", ordered, ok)
	}

	gateway := &gatewayPluginClient{wrapper: &clientWrapper{health: plugin.HealthUnhealthy}}
	if commands, err := gateway.Process(nil, plugin.PreUpstream, nil, "", "", "", "", false); err == nil || commands != nil {
		t.Fatalf("Process(unhealthy) = (%v,%v), want nil,error", commands, err)
	}
}

func TestBuildOrderCandidatesRequestMapsFields(t *testing.T) {
	req := buildOrderCandidatesRequest([]plugin.CandidateInfo{{
		Name:     "p1",
		Model:    "m1",
		Weight:   3,
		UnitCost: 0.2,
		Load:     7,
		TPM:      11,
		Healthy:  true,
	}}, plugin.RouteContext{
		Model:               "m1",
		SessionID:           "s1",
		InputText:           "hello",
		PromptTokens:        42,
		Stream:              true,
		HasTools:            true,
		HasImages:           true,
		HasStructuredOutput: true,
	})

	if len(req.Candidates) != 1 || req.Candidates[0].Name != "p1" || req.Candidates[0].Tpm != 11 {
		t.Fatalf("buildOrderCandidatesRequest candidates = %+v", req.Candidates)
	}
	if req.Context.SessionId != "s1" || !req.Context.HasTools || !req.Context.HasImages || !req.Context.HasStructuredOutput {
		t.Fatalf("buildOrderCandidatesRequest context = %+v", req.Context)
	}
}

func TestManagerCloseReturnsFirstError(t *testing.T) {
	conn, err := grpc.NewClient("passthrough:///unused", grpc.WithInsecure())
	if err != nil {
		t.Fatalf("grpc.NewClient() error: %v", err)
	}
	m := &Manager{clients: map[string]*clientWrapper{"p": {conn: conn, timeout: time.Millisecond}}}
	if err := m.Close(); err != nil {
		t.Fatalf("Manager.Close() error: %v", err)
	}
}
