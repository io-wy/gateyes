package provider

import (
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
)

func TestDefaultRegistryRecordFromConfigAndManagerFiltering(t *testing.T) {
	openaiCfg := config.ProviderConfig{
		Name:      "openai-a",
		Type:      "openai",
		BaseURL:   "https://openai.example",
		Endpoint:  "responses",
		Model:     "gpt-test",
		Enabled:   true,
		MaxTokens: 64000,
		Weight:    7,
	}
	record := DefaultRegistryRecordFromConfig(openaiCfg)
	if !record.SupportsChat || !record.SupportsResponses || !record.SupportsMessages || !record.SupportsLongContext || record.RoutingWeight != 7 {
		t.Fatalf("DefaultRegistryRecordFromConfig(openai) = %+v, want openai capability defaults", record)
	}

	anthropicCfg := config.ProviderConfig{
		Name:      "anthropic-a",
		Type:      "anthropic",
		BaseURL:   "https://anthropic.example",
		Model:     "claude-test",
		Enabled:   true,
		MaxTokens: 4096,
	}
	record = DefaultRegistryRecordFromConfig(anthropicCfg)
	if !record.SupportsMessages || !record.SupportsResponses || !record.SupportsChat || record.SupportsStructuredOutput {
		t.Fatalf("DefaultRegistryRecordFromConfig(anthropic) = %+v, want anthropic capability defaults", record)
	}

	grpcCfg := config.ProviderConfig{
		Name:       "grpc-vllm",
		Type:       "grpc",
		Vendor:     "vllm",
		GRPCTarget: "127.0.0.1:50051",
		Model:      "Qwen/Qwen3-8B",
		Enabled:    true,
		MaxTokens:  131072,
	}
	record = DefaultRegistryRecordFromConfig(grpcCfg)
	if record.SupportsChat || !record.SupportsResponses || record.SupportsMessages || record.SupportsTools || record.SupportsImages || !record.SupportsStructuredOutput || !record.SupportsLongContext {
		t.Fatalf("DefaultRegistryRecordFromConfig(grpc-vllm) = %+v, want grpc-vllm capability defaults", record)
	}

	manager, err := NewManager([]config.ProviderConfig{
		{
			Name:      "openai-a",
			Type:      "openai",
			BaseURL:   "https://openai.example",
			Endpoint:  "responses",
			APIKey:    "k1",
			Model:     "gpt-test",
			Timeout:   5,
			Enabled:   true,
			MaxTokens: 64000,
		},
		{
			Name:      "anthropic-a",
			Type:      "anthropic",
			BaseURL:   "https://anthropic.example",
			APIKey:    "k2",
			Model:     "claude-test",
			Timeout:   5,
			Enabled:   true,
			MaxTokens: 4096,
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error: %v", err)
	}

	manager.ApplyRegistry([]repository.ProviderRegistryRecord{
		{
			Name:                     "openai-a",
			Enabled:                  true,
			Drain:                    false,
			HealthStatus:             ProviderHealthHealthy,
			RoutingWeight:            1,
			SupportsStream:           true,
			SupportsTools:            true,
			SupportsImages:           true,
			SupportsStructuredOutput: true,
		},
		{
			Name:                     "anthropic-a",
			Enabled:                  true,
			Drain:                    true,
			HealthStatus:             ProviderHealthHealthy,
			RoutingWeight:            9,
			SupportsStream:           true,
			SupportsTools:            true,
			SupportsImages:           true,
			SupportsStructuredOutput: false,
		},
	})

	if got, ok := manager.Registry("openai-a"); !ok || got.Name != "openai-a" {
		t.Fatalf("Manager.Registry(openai-a) = (%+v,%v), want record", got, ok)
	}
	if got := manager.ListRegistry(); len(got) != 2 {
		t.Fatalf("Manager.ListRegistry() = %+v, want 2 records", got)
	}

	routable := manager.FilterRoutableByNames([]string{"openai-a", "anthropic-a"}, &ResponseRequest{
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
		Tools:    []any{map[string]any{"type": "function"}},
		Stream:   true,
	})
	if len(routable) != 1 || routable[0].Name() != "openai-a" {
		t.Fatalf("FilterRoutableByNames() = %v, want [openai-a] because anthropic-a is draining", providerNamesFromProviders(routable))
	}

	manager.ApplyRegistry([]repository.ProviderRegistryRecord{
		{
			Name:                     "anthropic-a",
			Enabled:                  true,
			Drain:                    false,
			HealthStatus:             ProviderHealthHealthy,
			RoutingWeight:            9,
			SupportsStream:           true,
			SupportsTools:            true,
			SupportsImages:           true,
			SupportsStructuredOutput: false,
		},
	})
	routable = manager.FilterRoutableByNames([]string{"openai-a", "anthropic-a"}, &ResponseRequest{
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	})
	if len(routable) != 2 || routable[0].Name() != "anthropic-a" || routable[1].Name() != "openai-a" {
		t.Fatalf("FilterRoutableByNames(weight order) = %v, want anthropic-a before openai-a", providerNamesFromProviders(routable))
	}
}

func TestFilterRoutableByNamesHonorsGRPCCapabilityBoundaries(t *testing.T) {
	manager, err := NewManager([]config.ProviderConfig{
		{
			Name:       "grpc-vllm",
			Type:       "grpc",
			Vendor:     "vllm",
			GRPCTarget: "127.0.0.1:50051",
			Model:      "Qwen/Qwen3-8B",
			Timeout:    5,
			Enabled:    true,
			MaxTokens:  131072,
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error: %v", err)
	}

	manager.ApplyRegistry([]repository.ProviderRegistryRecord{{
		Name:                     "grpc-vllm",
		Enabled:                  true,
		Drain:                    false,
		HealthStatus:             ProviderHealthHealthy,
		RoutingWeight:            1,
		SupportsResponses:        true,
		SupportsStream:           true,
		SupportsTools:            false,
		SupportsImages:           false,
		SupportsStructuredOutput: true,
	}})

	if got := manager.FilterRoutableByNames([]string{"grpc-vllm"}, &ResponseRequest{
		Surface:  "chat",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
		Tools:    []any{map[string]any{"type": "function"}},
	}); len(got) != 0 {
		t.Fatalf("FilterRoutableByNames(grpc tools) = %v, want filtered out", providerNamesFromProviders(got))
	}

	if got := manager.FilterRoutableByNames([]string{"grpc-vllm"}, &ResponseRequest{
		Surface:  "chat",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	}); len(got) != 0 {
		t.Fatalf("FilterRoutableByNames(grpc chat surface) = %v, want filtered out", providerNamesFromProviders(got))
	}

	if got := manager.FilterRoutableByNames([]string{"grpc-vllm"}, &ResponseRequest{
		Surface:  "messages",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	}); len(got) != 0 {
		t.Fatalf("FilterRoutableByNames(grpc messages surface) = %v, want filtered out", providerNamesFromProviders(got))
	}

	if got := manager.FilterRoutableByNames([]string{"grpc-vllm"}, &ResponseRequest{
		Messages: []Message{{
			Role: "user",
			Content: []ContentBlock{{
				Type:  "image",
				Image: &ContentImage{URL: "https://example.com/cat.png"},
			}},
		}},
	}); len(got) != 0 {
		t.Fatalf("FilterRoutableByNames(grpc images) = %v, want filtered out", providerNamesFromProviders(got))
	}

	if got := manager.FilterRoutableByNames([]string{"grpc-vllm"}, &ResponseRequest{
		Surface:  "responses",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
		OutputFormat: &OutputFormat{
			Type:   "json_schema",
			Schema: map[string]any{"type": "object"},
		},
		Stream: true,
	}); len(got) != 1 || got[0].Name() != "grpc-vllm" {
		t.Fatalf("FilterRoutableByNames(grpc structured output) = %v, want grpc-vllm", providerNamesFromProviders(got))
	}
}

func providerNamesFromProviders(items []Provider) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name())
	}
	return names
}
