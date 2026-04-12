package provider

import (
	"fmt"

	"github.com/gateyes/gateway/internal/config"
)

type Manager struct {
	providers       map[string]Provider
	defaultProvider string
	Stats           *Stats
}

func NewManager(cfg []config.ProviderConfig) (*Manager, error) {
	manager := &Manager{
		providers: make(map[string]Provider),
		Stats:     NewStats(),
	}

	for _, providerCfg := range cfg {
		if !providerCfg.Enabled {
			continue
		}

		instance, err := newProvider(providerCfg)
		if err != nil {
			return nil, err
		}

		manager.providers[providerCfg.Name] = instance
		manager.Stats.Register(instance)

		if manager.defaultProvider == "" {
			manager.defaultProvider = providerCfg.Name
		}
	}

	return manager, nil
}

func (m *Manager) Get(name string) (Provider, bool) {
	p, ok := m.providers[name]
	return p, ok
}

func (m *Manager) List() []Provider {
	result := make([]Provider, 0, len(m.providers))
	for _, provider := range m.providers {
		result = append(result, provider)
	}
	return result
}

func (m *Manager) ListByNames(names []string) []Provider {
	if len(names) == 0 {
		return nil
	}

	result := make([]Provider, 0, len(names))
	for _, name := range names {
		if provider, ok := m.providers[name]; ok {
			result = append(result, provider)
		}
	}
	return result
}

func (m *Manager) CloseIdleConnections() {
	if m == nil {
		return
	}
	for _, instance := range m.providers {
		if closer, ok := instance.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
}

func newProvider(cfg config.ProviderConfig) (Provider, error) {
	switch cfg.Type {
	case "openai", "azure", "":
		return NewOpenAIProvider(cfg), nil
	case "anthropic":
		return NewAnthropicProvider(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", cfg.Type)
	}
}
