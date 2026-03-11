package openai

import (
	"context"
	"net/http"
	"strings"

	"gateyes/internal/config"
	"gateyes/internal/provider"
	"gateyes/internal/provider/upstream"
)

type CompatibleProvider struct {
	name  string
	proxy *upstream.Proxy
}

func New(name string, cfg config.ProviderConfig) (*CompatibleProvider, error) {
	proxy, err := upstream.New(
		cfg.BaseURL,
		cfg.WSBaseURL,
		cfg.Headers,
		cfg.AuthHeader,
		cfg.AuthScheme,
		cfg.APIKey,
		"",
	)
	if err != nil {
		return nil, err
	}

	return &CompatibleProvider{
		name:  strings.ToLower(strings.TrimSpace(name)),
		proxy: proxy,
	}, nil
}

func (p *CompatibleProvider) Name() string {
	return p.name
}

func (p *CompatibleProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.proxy.ServeHTTP(w, r)
}

func (p *CompatibleProvider) ForwardRequest(
	r *http.Request,
	body []byte,
) (*http.Response, context.CancelFunc, error) {
	return p.proxy.ForwardRequest(r, body)
}

var _ provider.ModelProvider = (*CompatibleProvider)(nil)
