package responses

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/service/pricing"
	"github.com/gateyes/gateway/internal/service/provider"
)

// loadFeedFromMockHTTP spins a transient httptest server returning the
// supplied JSON, refreshes the feed once, and returns the populated Feed.
// Avoids poking unexported state.
func loadFeedFromMockHTTP(t *testing.T, body string) *pricing.Feed {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	feed := pricing.New(pricing.Options{URL: srv.URL})
	if err := feed.Refresh(context.Background()); err != nil {
		t.Fatalf("feed refresh: %v", err)
	}
	return feed
}

// pricingMockProvider lets us drive computeCost from a known provider.
type pricingMockProvider struct {
	name     string
	model    string
	priceIn  float64
	priceOut float64
}

func (p *pricingMockProvider) Name() string      { return p.name }
func (p *pricingMockProvider) Type() string      { return "mock" }
func (p *pricingMockProvider) Model() string     { return p.model }
func (p *pricingMockProvider) BaseURL() string   { return "" }
func (p *pricingMockProvider) Weight() int       { return 1 }
func (p *pricingMockProvider) UnitCost() float64 { return p.priceIn + p.priceOut }
func (p *pricingMockProvider) Cost(prompt, completion int) float64 {
	return float64(prompt)*p.priceIn + float64(completion)*p.priceOut
}
func (p *pricingMockProvider) CreateResponse(ctx context.Context, req *provider.ResponseRequest) (*provider.Response, error) {
	return nil, nil
}
func (p *pricingMockProvider) StreamResponse(ctx context.Context, req *provider.ResponseRequest) (<-chan provider.ResponseEvent, <-chan error) {
	out := make(chan provider.ResponseEvent)
	errCh := make(chan error, 1)
	close(out)
	close(errCh)
	return out, errCh
}
func (p *pricingMockProvider) CreateEmbedding(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, nil
}
func (p *pricingMockProvider) CreateImageGeneration(ctx context.Context, req *provider.ImageGenerationRequest) (*provider.ImageGenerationResponse, error) {
	return nil, nil
}

func TestComputeCostUsesProviderConfigWhenAvailable(t *testing.T) {
	svc := &Service{}
	p := &pricingMockProvider{name: "p1", model: "m1", priceIn: 0.1, priceOut: 0.2}
	got := svc.computeCost(p, "m1", 10, 5)
	want := 10*0.1 + 5*0.2
	if got != want {
		t.Fatalf("computeCost = %v, want %v", got, want)
	}
}

func TestComputeCostFallsBackToFeedWhenProviderHasNoPrice(t *testing.T) {
	feed := loadFeedFromMockHTTP(t, `{"gpt-4o-mini":{"input_cost_per_token":0.001,"output_cost_per_token":0.002}}`)
	svc := &Service{pricingFeed: feed}
	p := &pricingMockProvider{name: "p1", model: "m1"} // priceIn/Out = 0
	got := svc.computeCost(p, "gpt-4o-mini", 10, 5)
	want := 10*0.001 + 5*0.002
	if got != want {
		t.Fatalf("computeCost = %v, want %v (feed fallback)", got, want)
	}
}

func TestComputeCostPrefersProviderConfigOverFeed(t *testing.T) {
	feed := loadFeedFromMockHTTP(t, `{"gpt-4o-mini":{"input_cost_per_token":99.0,"output_cost_per_token":99.0}}`)
	svc := &Service{pricingFeed: feed}
	p := &pricingMockProvider{name: "p1", model: "gpt-4o-mini", priceIn: 0.1, priceOut: 0.2}
	got := svc.computeCost(p, "gpt-4o-mini", 10, 5)
	if got != 10*0.1+5*0.2 {
		t.Fatalf("computeCost = %v, expected provider yaml prices to win", got)
	}
}

func TestComputeCostReturnsZeroWhenNeitherSourceHasPrice(t *testing.T) {
	svc := &Service{}
	p := &pricingMockProvider{name: "p1", model: "m1"}
	if got := svc.computeCost(p, "unknown-model", 10, 5); got != 0 {
		t.Fatalf("computeCost = %v, want 0 when no price source available", got)
	}
}
