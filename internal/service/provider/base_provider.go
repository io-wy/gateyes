package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gateyes/gateway/internal/app/config"
)

type baseProvider struct {
	cfg          config.ProviderConfig
	client       *http.Client
	modelAliases map[string]string
}

func newBaseProvider(cfg config.ProviderConfig) baseProvider {
	aliases := make(map[string]string, len(cfg.ModelAliases))
	for k, v := range cfg.ModelAliases {
		if k != "" && v != "" {
			aliases[k] = v
		}
	}
	return baseProvider{
		cfg:          cfg,
		client:       newProviderHTTPClient(cfg.Timeout),
		modelAliases: aliases,
	}
}

func (p *baseProvider) ResolveModel(requested string) string {
	return resolveModel(p.modelAliases, requested)
}

// resolveModel maps an incoming model name to the provider-specific model name
// using configured aliases. If no alias matches, the requested name is returned.
// Resolution is bounded to prevent alias cycles; on cycle detection the last
// resolved name before repetition is returned.
func resolveModel(aliases map[string]string, requested string) string {
	if requested == "" || len(aliases) == 0 {
		return requested
	}
	seen := make(map[string]struct{})
	current := requested
	for i := 0; i < 5; i++ {
		if _, ok := seen[current]; ok {
			break
		}
		seen[current] = struct{}{}
		target, ok := aliases[current]
		if !ok || target == "" {
			return current
		}
		current = target
	}
	return current
}

func (p *baseProvider) Name() string {
	return p.cfg.Name
}

func (p *baseProvider) Type() string {
	return p.cfg.Type
}

func (p *baseProvider) BaseURL() string {
	return p.cfg.BaseURL
}

func (p *baseProvider) Model() string {
	return p.cfg.Model
}

func (p *baseProvider) Weight() int {
	return p.cfg.Weight
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

func (p *baseProvider) CreateEmbedding(ctx context.Context, req *EmbeddingRequest) (*EmbeddingResponse, error) {
	return nil, fmt.Errorf("provider %s does not support embeddings", p.cfg.Name)
}

func (p *baseProvider) CreateImageGeneration(ctx context.Context, req *ImageGenerationRequest) (*ImageGenerationResponse, error) {
	return nil, fmt.Errorf("provider %s does not support image generation", p.cfg.Name)
}
