package provider

import (
	"strings"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
)

const (
	ProviderHealthHealthy   = "healthy"
	ProviderHealthDegraded  = "degraded"
	ProviderHealthUnhealthy = "unhealthy"
)

func DefaultRegistryRecordFromConfig(cfg config.ProviderConfig) repository.ProviderRegistryRecord {
	weight := cfg.Weight
	if weight <= 0 {
		weight = 1
	}

	providerType := strings.ToLower(strings.TrimSpace(cfg.Type))
	endpoint := strings.ToLower(strings.TrimSpace(cfg.Endpoint))
	record := repository.ProviderRegistryRecord{
		Name:                     cfg.Name,
		Type:                     cfg.Type,
		Vendor:                   cfg.Vendor,
		BaseURL:                  cfg.BaseURL,
		Endpoint:                 cfg.Endpoint,
		Model:                    cfg.Model,
		Enabled:                  cfg.Enabled,
		Drain:                    false,
		HealthStatus:             ProviderHealthHealthy,
		RoutingWeight:            weight,
		SupportsStream:           true,
		SupportsTools:            true,
		SupportsImages:           true,
		SupportsStructuredOutput: providerType != "anthropic",
		SupportsLongContext:      cfg.MaxTokens >= 32000,
		SupportsEmbeddings:       endpoint == "embeddings",
		RuntimeConfig:            runtimeConfigFromProviderConfig(cfg),
	}

	switch providerType {
	case "anthropic":
		record.SupportsChat = true
		record.SupportsResponses = true
		record.SupportsMessages = true
	case "openai", "azure", "":
		record.SupportsChat = true
		record.SupportsResponses = true
		record.SupportsMessages = true
	default:
		record.SupportsChat = true
	}

	applyCapabilityOverrides(&record, cfg.Capabilities)
	return record
}

func applyCapabilityOverrides(record *repository.ProviderRegistryRecord, caps config.ProviderCapabilitiesConfig) {
	if caps.Chat != nil {
		record.SupportsChat = *caps.Chat
	}
	if caps.Responses != nil {
		record.SupportsResponses = *caps.Responses
	}
	if caps.Messages != nil {
		record.SupportsMessages = *caps.Messages
	}
	if caps.Stream != nil {
		record.SupportsStream = *caps.Stream
	}
	if caps.Tools != nil {
		record.SupportsTools = *caps.Tools
	}
	if caps.Images != nil {
		record.SupportsImages = *caps.Images
	}
	if caps.StructuredOutput != nil {
		record.SupportsStructuredOutput = *caps.StructuredOutput
	}
	if caps.LongContext != nil {
		record.SupportsLongContext = *caps.LongContext
	}
	if caps.Embeddings != nil {
		record.SupportsEmbeddings = *caps.Embeddings
	}
}

func runtimeConfigFromProviderConfig(cfg config.ProviderConfig) *repository.ProviderRuntimeConfig {
	return &repository.ProviderRuntimeConfig{
		APIKey:       cfg.APIKey,
		PriceInput:   cfg.PriceInput,
		PriceOutput:  cfg.PriceOutput,
		MaxTokens:    cfg.MaxTokens,
		Timeout:      cfg.Timeout,
		Enabled:      cfg.Enabled,
		Headers:      cfg.Headers,
		ExtraBody:    cfg.ExtraBody,
		ModelAliases: cfg.ModelAliases,
	}
}

func registryAllowsRequest(record repository.ProviderRegistryRecord, req *ResponseRequest) bool {
	if !record.Enabled || record.Drain {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(record.HealthStatus)) {
	case "", ProviderHealthHealthy, ProviderHealthDegraded:
	default:
		return false
	}
	if req == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(req.Surface)) {
	case "chat":
		if !record.SupportsChat {
			return false
		}
	case "responses":
		if !record.SupportsResponses {
			return false
		}
	case "messages":
		if !record.SupportsMessages {
			return false
		}
	}
	if req.Stream && !record.SupportsStream {
		return false
	}
	if req.HasToolsRequested() && !record.SupportsTools {
		return false
	}
	if req.HasImageInput() && !record.SupportsImages {
		return false
	}
	if req.HasStructuredOutputRequest() && !record.SupportsStructuredOutput {
		return false
	}
	return true
}
