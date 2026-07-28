package provider

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/gateyes/gateway/internal/app/config"
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
	m.mu.RLock()
	defer m.mu.RUnlock()
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
	m.mu.RLock()
	defer m.mu.RUnlock()
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

	m.mu.RLock()
	defer m.mu.RUnlock()
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
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, instance := range m.providers {
		if closer, ok := instance.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
}

func (m *Manager) UpsertRuntimeProvider(record repository.ProviderRegistryRecord) error {
	if m == nil {
		return nil
	}
	cfg := providerConfigFromRegistry(record)
	m.mu.Lock()
	defer m.mu.Unlock()
	if record.Enabled {
		instance, err := newProvider(cfg)
		if err != nil {
			return err
		}
		if existing, ok := m.providers[record.Name]; ok {
			closeProviderIdleConnections(existing)
		}
		m.providers[record.Name] = instance
		if m.Stats != nil {
			m.Stats.Register(instance)
			m.Stats.SetStatus(record.Name, firstNonEmptyHealth(record.HealthStatus))
		}
		if m.defaultProvider == "" {
			m.defaultProvider = record.Name
		}
	} else {
		if existing, ok := m.providers[record.Name]; ok {
			closeProviderIdleConnections(existing)
			delete(m.providers, record.Name)
		}
	}
	m.registry[record.Name] = record
	return nil
}

func (m *Manager) RemoveRuntimeProvider(name string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.providers[name]; ok {
		closeProviderIdleConnections(existing)
		delete(m.providers, name)
	}
	delete(m.registry, name)
	if m.Stats != nil {
		m.Stats.Unregister(name)
	}
	if m.defaultProvider == name {
		m.defaultProvider = ""
		for candidate := range m.providers {
			m.defaultProvider = candidate
			break
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
	return nil
}

func firstNonEmptyHealth(value string) string {
	if strings.TrimSpace(value) == "" {
		return ProviderHealthHealthy
	}
	return value
}

func providerConfigFromRegistry(record repository.ProviderRegistryRecord) config.ProviderConfig {
	cfg := config.ProviderConfig{
		Name:     record.Name,
		Type:     record.Type,
		Vendor:   record.Vendor,
		BaseURL:  record.BaseURL,
		Endpoint: record.Endpoint,
		Model:    record.Model,
		Weight:   record.RoutingWeight,
		Enabled:  record.Enabled,
	}
	if record.RuntimeConfig != nil {
		cfg.APIKey = record.RuntimeConfig.APIKey
		cfg.PriceInput = record.RuntimeConfig.PriceInput
		cfg.PriceOutput = record.RuntimeConfig.PriceOutput
		cfg.MaxTokens = record.RuntimeConfig.MaxTokens
		cfg.Timeout = record.RuntimeConfig.Timeout
		cfg.Headers = cloneStringMap(record.RuntimeConfig.Headers)
		cfg.ExtraBody = cloneAnyMap(record.RuntimeConfig.ExtraBody)
		cfg.ModelAliases = cloneStringMap(record.RuntimeConfig.ModelAliases)
	}
	return cfg
}

func closeProviderIdleConnections(instance Provider) {
	if closer, ok := instance.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (m *Manager) Name() string { return "provider" }

func (m *Manager) Reload(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	newProviders := make(map[string]Provider, len(cfg.Providers))
	newRegistry := make(map[string]repository.ProviderRegistryRecord, len(cfg.Providers))

	for _, providerCfg := range cfg.Providers {
		record := DefaultRegistryRecordFromConfig(providerCfg)
		newRegistry[providerCfg.Name] = record

		if !providerCfg.Enabled {
			continue
		}

		instance, err := newProvider(providerCfg)
		if err != nil {
			return fmt.Errorf("create provider %s: %w", providerCfg.Name, err)
		}
		newProviders[providerCfg.Name] = instance
		if m.Stats != nil {
			m.Stats.Register(instance)
			m.Stats.SetStatus(providerCfg.Name, firstNonEmptyHealth(record.HealthStatus))
		}
	}

	for name, existing := range m.providers {
		if _, ok := newProviders[name]; !ok {
			closeProviderIdleConnections(existing)
			if m.Stats != nil {
				m.Stats.Unregister(name)
			}
		} else {
			closeProviderIdleConnections(existing)
		}
	}

	m.providers = newProviders
	m.registry = newRegistry

	m.defaultProvider = ""
	for _, providerCfg := range cfg.Providers {
		if providerCfg.Enabled {
			m.defaultProvider = providerCfg.Name
			break
		}
	}

	return nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
