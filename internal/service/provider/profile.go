package provider

import (
	"net/http"
	"strings"

	"github.com/gateyes/gateway/internal/config"
)

func applyProviderProfile(cfg config.ProviderConfig, payload map[string]any, headers http.Header) {
	applyProviderPayloadProfile(cfg, payload)
	for key, value := range cfg.Headers {
		if strings.TrimSpace(key) == "" || value == "" {
			continue
		}
		headers.Set(key, value)
	}
	if vendor := strings.TrimSpace(cfg.Vendor); vendor != "" {
		headers.Set("X-Gateyes-Vendor", vendor)
	}
}

func applyProviderPayloadProfile(cfg config.ProviderConfig, payload map[string]any) {
	applyVendorProfileDefaults(cfg, payload, nil)
	mergeAnyMap(payload, cfg.ExtraBody)
}

func applyVendorProfileDefaults(cfg config.ProviderConfig, payload map[string]any, headers http.Header) {
	vendor := strings.ToLower(strings.TrimSpace(cfg.Vendor))
	if vendor == "" {
		return
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "openai", "azure", "":
		applyOpenAIVendorDefaults(vendor, payload, headers)
	case "anthropic":
		applyAnthropicVendorDefaults(vendor, payload, headers)
	}
}

func applyOpenAIVendorDefaults(vendor string, payload map[string]any, headers http.Header) {
	switch vendor {
	case "vllm":
		setDefaultPayloadValue(payload, "top_k", -1)
	case "ollama":
		setDefaultPayloadValue(payload, "stream_options", map[string]any{"include_usage": true})
	}
}

func applyAnthropicVendorDefaults(vendor string, payload map[string]any, headers http.Header) {
	switch vendor {
	case "minimax":
		setDefaultPayloadValue(payload, "stream_options", map[string]any{"include_usage": true})
	}
}

func setDefaultPayloadValue(payload map[string]any, key string, value any) {
	if payload == nil || key == "" {
		return
	}
	if _, exists := payload[key]; exists {
		return
	}
	payload[key] = cloneAnyValue(value)
}

func mergeAnyMap(dst map[string]any, src map[string]any) {
	if dst == nil || len(src) == 0 {
		return
	}
	for key, value := range src {
		if existing, ok := dst[key].(map[string]any); ok {
			if nested, ok := value.(map[string]any); ok {
				mergeAnyMap(existing, nested)
				continue
			}
		}
		dst[key] = cloneAnyValue(value)
	}
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, item := range typed {
			cloned[key] = cloneAnyValue(item)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for i, item := range typed {
			cloned[i] = cloneAnyValue(item)
		}
		return cloned
	default:
		return value
	}
}
