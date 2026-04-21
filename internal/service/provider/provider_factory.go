package provider

import (
	"strings"

	"github.com/gateyes/gateway/internal/config"
)

type providerFactory func(config.ProviderConfig) Provider

func resolveProviderFactory(providerType string) (providerFactory, error) {
	switch normalizeProviderType(providerType) {
	case "openai", "azure":
		return NewOpenAIProvider, nil
	case "anthropic":
		return NewAnthropicProvider, nil
	case "grpc":
		return NewGRPCProvider, nil
	default:
		return nil, newProviderConfigError("provider.factory", "unsupported provider type: "+providerType)
	}
}

func normalizeProviderType(providerType string) string {
	normalized := strings.ToLower(strings.TrimSpace(providerType))
	if normalized == "" {
		return "openai"
	}
	return normalized
}
