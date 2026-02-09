package gateway

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"gateyes/internal/config"
)

type OpenAIProxy struct {
	providers       map[string]*UpstreamProxy
	providerHeader  string
	providerQuery   string
	defaultProvider string
}

func NewOpenAIProxy(cfg config.GatewayConfig, providers map[string]config.ProviderConfig) (*OpenAIProxy, error) {
	registry := make(map[string]*UpstreamProxy)
	for name, provider := range providers {
		normalized := normalizeProviderName(name)
		if normalized == "" {
			continue
		}
		proxy, err := NewUpstreamProxy(provider.BaseURL, provider.WSBaseURL, provider.Headers, provider.AuthHeader, provider.AuthScheme, provider.APIKey, "")
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}
		registry[normalized] = proxy
	}

	if len(registry) == 0 {
		slog.Warn("no providers configured")
	}

	return &OpenAIProxy{
		providers:       registry,
		providerHeader:  cfg.ProviderHeader,
		providerQuery:   cfg.ProviderQuery,
		defaultProvider: cfg.DefaultProvider,
	}, nil
}

func (o *OpenAIProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	provider := o.resolveProvider(r)
	if provider == "" {
		http.Error(w, "missing provider", http.StatusBadRequest)
		return
	}
	proxy, ok := o.providers[provider]
	if !ok {
		http.Error(w, "unknown provider", http.StatusBadRequest)
		return
	}
	proxy.ServeHTTP(w, r)
}

func (o *OpenAIProxy) resolveProvider(r *http.Request) string {
	if o.providerHeader != "" {
		if value := strings.TrimSpace(r.Header.Get(o.providerHeader)); value != "" {
			return normalizeProviderName(value)
		}
	}
	if o.providerQuery != "" {
		if value := strings.TrimSpace(r.URL.Query().Get(o.providerQuery)); value != "" {
			return normalizeProviderName(value)
		}
	}
	return normalizeProviderName(o.defaultProvider)
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
