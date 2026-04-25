package provider

import (
	"strings"

	"github.com/gateyes/gateway/internal/config"
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
		SupportsEmbeddings:       providerType == "openai" || providerType == "azure" || providerType == "",
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
	case "grpc":
		record.SupportsResponses = true
		record.SupportsChat = false
		record.SupportsMessages = false
		record.SupportsTools = false
		record.SupportsImages = false
		record.SupportsStructuredOutput = strings.EqualFold(strings.TrimSpace(cfg.Vendor), "vllm")
	default:
		record.SupportsChat = true
	}

	return record
}

func runtimeConfigFromProviderConfig(cfg config.ProviderConfig) *repository.ProviderRuntimeConfig {
	return &repository.ProviderRuntimeConfig{
		GRPCTarget:    cfg.GRPCTarget,
		GRPCUseTLS:    cfg.GRPCUseTLS,
		GRPCAuthority: cfg.GRPCAuthority,
		APIKey:        cfg.APIKey,
		PriceInput:    cfg.PriceInput,
		PriceOutput:   cfg.PriceOutput,
		MaxTokens:     cfg.MaxTokens,
		Timeout:       cfg.Timeout,
		Enabled:       cfg.Enabled,
		Headers:       cfg.Headers,
		ExtraBody:     cfg.ExtraBody,
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
