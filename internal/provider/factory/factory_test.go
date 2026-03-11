package factory

import (
	"testing"

	"gateyes/internal/config"
)

func TestNewModelRegistryDefaultsToOpenAIType(t *testing.T) {
	registry, err := NewModelRegistry(map[string]config.ProviderConfig{
		"openai-primary": {
			BaseURL: "https://api.openai.com",
		},
	})
	if err != nil {
		t.Fatalf("NewModelRegistry failed: %v", err)
	}

	if !registry.Has("openai-primary") {
		t.Fatalf("expected provider to be registered")
	}
}

func TestNewModelRegistryRejectsUnsupportedType(t *testing.T) {
	_, err := NewModelRegistry(map[string]config.ProviderConfig{
		"claude": {
			Type:    "anthropic",
			BaseURL: "https://api.anthropic.com",
		},
	})
	if err == nil {
		t.Fatalf("expected unsupported provider type error")
	}
}
