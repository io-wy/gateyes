package gateway

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"gateyes/internal/config"
)

type AnthropicProxy struct {
	providers map[string]*UpstreamProxy
}

func NewAnthropicProxy(cfg config.GatewayConfig, providers map[string]config.ProviderConfig) (*AnthropicProxy, error) {
	registry := make(map[string]*UpstreamProxy)

	anthropicProvider := cfg.AnthropicProvider
	if anthropicProvider == "" {
		anthropicProvider = "anthropic"
	}

	for name, provider := range providers {
		normalized := normalizeProviderName(name)
		if normalized == "" {
			continue
		}
		if normalized != anthropicProvider {
			continue
		}
		proxy, err := NewUpstreamProxy(provider.BaseURL, provider.WSBaseURL, provider.Headers, provider.AuthHeader, provider.AuthScheme, provider.APIKey, "")
		if err != nil {
			return nil, fmt.Errorf("anthropic provider %q: %w", name, err)
		}
		registry[normalized] = proxy
	}

	if len(registry) == 0 {
		slog.Warn("no anthropic provider configured")
	}

	return &AnthropicProxy{
		providers: registry,
	}, nil
}

func (a *AnthropicProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	provider := "anthropic"
	proxy, ok := a.providers[provider]
	if !ok {
		http.Error(w, "anthropic provider not configured", http.StatusServiceUnavailable)
		return
	}

	path := r.URL.Path
	if !strings.HasPrefix(path, "/v1/") {
		path = "/v1" + path
		r.URL.Path = path
	}

	proxy.ServeHTTP(w, r)
}
