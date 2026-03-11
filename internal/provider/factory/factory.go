package factory

import (
	"fmt"
	"strings"

	"gateyes/internal/config"
	"gateyes/internal/provider"
	provideropenai "gateyes/internal/provider/openai"
)

func NewModelRegistry(configs map[string]config.ProviderConfig) (*provider.Registry, error) {
	registry := provider.NewRegistry()

	for name, cfg := range configs {
		providerType := strings.ToLower(strings.TrimSpace(cfg.Type))
		if providerType == "" {
			providerType = provider.TypeOpenAI
		}

		switch providerType {
		case provider.TypeOpenAI:
			adapter, err := provideropenai.New(name, cfg)
			if err != nil {
				return nil, fmt.Errorf("provider %q: %w", name, err)
			}
			if err := registry.Register(adapter); err != nil {
				return nil, fmt.Errorf("provider %q: %w", name, err)
			}
		default:
			return nil, fmt.Errorf("provider %q has unsupported type %q", name, cfg.Type)
		}
	}

	return registry, nil
}
