package wasm

import (
	"log/slog"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/plugin"
)

// Manager manages WASM gateway plugins.
type Manager struct {
	plugins []*GatewayPlugin
	logger  *slog.Logger
}

// NewManager creates a manager from config. It loads each WASM plugin file
// and instantiates a GatewayPlugin.
func NewManager(cfgs []config.WASMPluginConfig) (*Manager, error) {
	m := &Manager{
		logger: slog.With("component", "wasm_plugin_manager"),
	}
	for _, c := range cfgs {
		if c.Path == "" {
			m.logger.Warn("wasm plugin missing path, skipping", "name", c.Name)
			continue
		}
		p, err := NewGatewayPlugin(c.Name, c.Path, c.Phases, c.TimeoutMs, c.MemoryPages)
		if err != nil {
			m.logger.Warn("failed to load wasm plugin, skipping", "name", c.Name, "path", c.Path, "error", err)
			continue
		}
		m.plugins = append(m.plugins, p)
		m.logger.Info("loaded wasm plugin", "name", c.Name, "phases", c.Phases)
	}
	return m, nil
}

// GetByPhase returns all WASM gateway plugins subscribed to the given phase.
func (m *Manager) GetByPhase(phase plugin.Phase) []plugin.Gateway {
	var result []plugin.Gateway
	phaseStr := string(phase)
	for _, p := range m.plugins {
		for _, subscribed := range p.phases {
			if subscribed == phaseStr {
				result = append(result, p)
				break
			}
		}
	}
	return result
}

// Close releases all WASM runtimes.
func (m *Manager) Close() error {
	var firstErr error
	for _, p := range m.plugins {
		if err := p.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
