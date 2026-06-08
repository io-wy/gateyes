package responses

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

// sfMockProvider counts CreateResponse invocations and sleeps so concurrent
// callers can pile up while the first one is still in flight.
type sfMockProvider struct {
	name     string
	model    string
	delay    time.Duration
	calls    atomic.Int64
	respText string
}

func (p *sfMockProvider) Name() string      { return p.name }
func (p *sfMockProvider) Type() string      { return "mock" }
func (p *sfMockProvider) Model() string     { return p.model }
func (p *sfMockProvider) BaseURL() string   { return "" }
func (p *sfMockProvider) Weight() int       { return 1 }
func (p *sfMockProvider) UnitCost() float64 { return 0 }
func (p *sfMockProvider) Cost(promptTokens, completionTokens int) float64 {
	return 0
}
func (p *sfMockProvider) StreamResponse(ctx context.Context, req *provider.ResponseRequest) (<-chan provider.ResponseEvent, <-chan error) {
	out := make(chan provider.ResponseEvent)
	errCh := make(chan error, 1)
	close(out)
	close(errCh)
	return out, errCh
}
func (p *sfMockProvider) CreateEmbedding(ctx context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, nil
}
func (p *sfMockProvider) CreateImageGeneration(ctx context.Context, req *provider.ImageGenerationRequest) (*provider.ImageGenerationResponse, error) {
	return nil, nil
}

func (p *sfMockProvider) CreateResponse(ctx context.Context, req *provider.ResponseRequest) (*provider.Response, error) {
	p.calls.Add(1)
	if p.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(p.delay):
		}
	}
	return &provider.Response{
		ID:     "resp-from-mock",
		Object: "response",
		Model:  p.model,
		Status: "completed",
		Output: []provider.ResponseOutput{{
			Type:    "message",
			Role:    "assistant",
			Content: []provider.ResponseContent{{Type: "output_text", Text: p.respText}},
		}},
		Usage: provider.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	}, nil
}

// TestCallWithRetrySFDedupesConcurrentMisses ensures that when two callers
// race on the same cache key + provider, only one upstream HTTP roundtrip
// is made. Both callers must receive a usable response.
func TestCallWithRetrySFDedupesConcurrentMisses(t *testing.T) {
	mock := &sfMockProvider{name: "p1", model: "m1", delay: 80 * time.Millisecond, respText: "shared"}
	svc := New(&Dependencies{
		Config: &config.Config{
			Retry: config.RetryConfig{MaxRetries: 0, InitialDelayMs: 1, MaxDelayMs: 1, BackoffFactor: 1},
			Cache: config.CacheConfig{Enabled: true, Singleflight: true, DefaultTTL: 60},
		},
		Cache:   newMockCache(),
		Metrics: &mockCacheMetrics{},
	})

	identity := &repository.AuthIdentity{TenantID: "t1", UserID: "u1"}
	req := &provider.ResponseRequest{Model: "m1", Input: "same prompt"}
	exec := &execution{
		provider:        mock,
		requestedModel:  "m1",
		upstreamRequest: &provider.ResponseRequest{Model: "m1", Input: "same prompt"},
		responseID:      "rid",
		startedAt:       time.Now(),
	}

	const callers = 5
	var wg sync.WaitGroup
	results := make([]*provider.Response, callers)
	errs := make([]error, callers)
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func(idx int) {
			defer wg.Done()
			resp, _, err := svc.callWithRetrySF(context.Background(), identity, exec, identity.TenantID, req)
			results[idx] = resp
			errs[idx] = err
		}(i)
		// stagger slightly to ensure all enter while first is in flight
		time.Sleep(2 * time.Millisecond)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d error: %v", i, err)
		}
		if results[i] == nil || results[i].ID != "resp-from-mock" {
			t.Fatalf("caller %d response = %+v, want shared mock response", i, results[i])
		}
	}
	if got := mock.calls.Load(); got != 1 {
		t.Fatalf("upstream invocations = %d, want 1 (singleflight failed to dedupe)", got)
	}
}

// TestCallWithRetrySFDisabledByConfig verifies that when Singleflight is
// false, every caller hits upstream independently (current default).
func TestCallWithRetrySFDisabledByConfig(t *testing.T) {
	mock := &sfMockProvider{name: "p1", model: "m1", delay: 30 * time.Millisecond}
	svc := New(&Dependencies{
		Config: &config.Config{
			Retry: config.RetryConfig{MaxRetries: 0, InitialDelayMs: 1, MaxDelayMs: 1, BackoffFactor: 1},
			Cache: config.CacheConfig{Enabled: true, Singleflight: false},
		},
		Cache:   newMockCache(),
		Metrics: &mockCacheMetrics{},
	})

	identity := &repository.AuthIdentity{TenantID: "t1", UserID: "u1"}
	req := &provider.ResponseRequest{Model: "m1", Input: "p"}
	exec := &execution{
		provider:        mock,
		requestedModel:  "m1",
		upstreamRequest: &provider.ResponseRequest{Model: "m1", Input: "p"},
		responseID:      "rid",
		startedAt:       time.Now(),
	}

	const callers = 3
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			_, _, _ = svc.callWithRetrySF(context.Background(), identity, exec, identity.TenantID, req)
		}()
	}
	wg.Wait()

	if got := mock.calls.Load(); got != callers {
		t.Fatalf("upstream invocations = %d, want %d", got, callers)
	}
}
