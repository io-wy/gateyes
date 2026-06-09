package wasm

import (
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/domain/plugin"
)

func TestNewGatewayPluginFromBytesRejectsInvalidWASM(t *testing.T) {
	if _, err := NewGatewayPluginFromBytes("bad", []byte("not-wasm"), nil, 0, 0); err == nil {
		t.Fatal("NewGatewayPluginFromBytes(invalid) error = nil, want error")
	}
}

func TestManagerSkipsMissingOrInvalidPlugins(t *testing.T) {
	m, err := NewManager([]config.WASMPluginConfig{
		{Name: "missing-path"},
		{Name: "bad", Path: "does-not-exist.wasm", Phases: []string{string(plugin.PreUpstream)}},
	})
	if err != nil {
		t.Fatalf("NewManager() error: %v", err)
	}
	if got := m.GetByPhase(plugin.PreUpstream); len(got) != 0 {
		t.Fatalf("GetByPhase(pre_upstream) len = %d, want 0", len(got))
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Manager.Close() error: %v", err)
	}
}

func TestManagerGetByPhaseFiltersSubscriptions(t *testing.T) {
	m := &Manager{plugins: []*GatewayPlugin{
		{name: "pre", phases: []string{string(plugin.PreUpstream)}},
		{name: "post", phases: []string{string(plugin.PostUpstream)}},
	}}

	got := m.GetByPhase(plugin.PreUpstream)
	if len(got) != 1 || got[0].Name() != "pre" {
		t.Fatalf("GetByPhase(pre_upstream) = %+v, want pre plugin", got)
	}
	if got := m.GetByPhase(plugin.Audit); len(got) != 0 {
		t.Fatalf("GetByPhase(audit) len = %d, want 0", len(got))
	}
}

func TestGatewayPluginMetadata(t *testing.T) {
	g := &GatewayPlugin{name: "meta", phases: []string{string(plugin.Audit)}}
	if g.Name() != "meta" || g.Type() != "gateway" || g.Health() != plugin.HealthHealthy {
		t.Fatalf("GatewayPlugin metadata = name:%s type:%s health:%v", g.Name(), g.Type(), g.Health())
	}
	if len(g.Phases()) != 1 || g.Phases()[0] != string(plugin.Audit) {
		t.Fatalf("GatewayPlugin phases = %v, want audit", g.Phases())
	}
}
