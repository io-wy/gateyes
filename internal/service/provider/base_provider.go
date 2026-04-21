package provider

import (
	"net/http"

	"github.com/gateyes/gateway/internal/config"
)

type baseProvider struct {
	cfg    config.ProviderConfig
	client *http.Client
}

func newBaseProvider(cfg config.ProviderConfig) baseProvider {
	return baseProvider{
		cfg:    cfg,
		client: newProviderHTTPClient(cfg.Timeout),
	}
}

func (p *baseProvider) Name() string {
	return p.cfg.Name
}

func (p *baseProvider) Type() string {
	return p.cfg.Type
}

func (p *baseProvider) BaseURL() string {
	if p.cfg.BaseURL != "" {
		return p.cfg.BaseURL
	}
	return p.cfg.GRPCTarget
}

func (p *baseProvider) Model() string {
	return p.cfg.Model
}

func (p *baseProvider) UnitCost() float64 {
	return p.cfg.PriceInput + p.cfg.PriceOutput
}

func (p *baseProvider) Cost(prompt, completion int) float64 {
	return float64(prompt)*p.cfg.PriceInput + float64(completion)*p.cfg.PriceOutput
}

func (p *baseProvider) CloseIdleConnections() {
	if p == nil || p.client == nil {
		return
	}
	p.client.CloseIdleConnections()
}
