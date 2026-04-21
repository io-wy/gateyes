package provider

import (
	"sort"
	"strings"
	"sync"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
)

type Manager struct {
	providers       map[string]Provider
	defaultProvider string
	Stats           *Stats
	registry        map[string]repository.ProviderRegistryRecord
	mu              sync.RWMutex
}

func NewManager(cfg []config.ProviderConfig) (*Manager, error) {
	manager := &Manager{
		providers: make(map[string]Provider),
		Stats:     NewStats(),
		registry:  make(map[string]repository.ProviderRegistryRecord),
	}

	for _, providerCfg := range cfg {
		manager.registry[providerCfg.Name] = DefaultRegistryRecordFromConfig(providerCfg)
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

func (m *Manager) ApplyRegistry(records []repository.ProviderRegistryRecord) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, record := range records {
		if strings.TrimSpace(record.Name) == "" {
			continue
		}
		m.registry[record.Name] = record
		if m.Stats != nil {
			m.Stats.SetStatus(record.Name, firstNonEmptyHealth(record.HealthStatus))
		}
	}
}

func (m *Manager) Registry(name string) (repository.ProviderRegistryRecord, bool) {
	if m == nil {
		return repository.ProviderRegistryRecord{}, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.registry[name]
	return record, ok
}

func (m *Manager) ListRegistry() []repository.ProviderRegistryRecord {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]repository.ProviderRegistryRecord, 0, len(m.registry))
	for _, record := range m.registry {
		result = append(result, record)
	}
	return result
}

func (m *Manager) FilterRoutableByNames(names []string, req *ResponseRequest) []Provider {
	if m == nil || len(names) == 0 {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Provider, 0, len(names))
	weights := make(map[string]int, len(names))
	for _, name := range names {
		instance, ok := m.providers[name]
		if !ok {
			continue
		}
		record, ok := m.registry[name]
		if ok && !registryAllowsRequest(record, req) {
			continue
		}
		if ok {
			weights[name] = record.RoutingWeight
		}
		result = append(result, instance)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return weights[result[i].Name()] > weights[result[j].Name()]
	})
	return result
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
	if err := validateProviderConfig(cfg); err != nil {
		return nil, err
	}
	factory, err := resolveProviderFactory(cfg.Type)
	if err != nil {
		return nil, err
	}
	return factory(cfg), nil
}

func validateProviderConfig(cfg config.ProviderConfig) error {
	if normalizeProviderType(cfg.Type) != "grpc" {
		return nil
	}
	if strings.TrimSpace(cfg.GRPCTarget) == "" {
		return newProviderConfigError("provider.grpc.config", "grpcTarget is required for grpc providers")
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Vendor)) {
	case "vllm":
		return nil
	case "":
		return newProviderConfigError("provider.grpc.config", "vendor is required for grpc providers")
	default:
		return newProviderConfigError("provider.grpc.config", "unsupported grpc vendor: "+cfg.Vendor)
	}
}

func firstNonEmptyHealth(value string) string {
	if strings.TrimSpace(value) == "" {
		return ProviderHealthHealthy
	}
	return value
}
