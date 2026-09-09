package inference

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/provider"
)

type fakeProvider struct {
	name  string
	resp  *provider.Response
	err   error
	mu    sync.Mutex
	calls int
}

func (p *fakeProvider) Name() string              { return p.name }
func (p *fakeProvider) Type() string              { return "fake" }
func (p *fakeProvider) BaseURL() string           { return "" }
func (p *fakeProvider) Model() string             { return "model" }
func (p *fakeProvider) Labels() map[string]string { return nil }
func (p *fakeProvider) Weight() int               { return 1 }
func (p *fakeProvider) UnitCost() float64         { return 0 }
func (p *fakeProvider) Cost(_, _ int) float64     { return 0 }
func (p *fakeProvider) CreateResponse(context.Context, *provider.ResponseRequest) (*provider.Response, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return p.resp, p.err
}
func (p *fakeProvider) StreamResponse(context.Context, *provider.ResponseRequest) (<-chan provider.ResponseEvent, <-chan error) {
	return nil, nil
}
func (p *fakeProvider) CreateEmbedding(context.Context, *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, nil
}
func (p *fakeProvider) CreateImageGeneration(context.Context, *provider.ImageGenerationRequest) (*provider.ImageGenerationResponse, error) {
	return nil, nil
}

type fakeRouter struct{ candidates []provider.Provider }

func (r fakeRouter) Plan(context.Context, *repository.AuthIdentity, *provider.ResponseRequest, string) ([]provider.Provider, error) {
	return r.candidates, nil
}

type fakeAdmission struct {
	err   error
	calls int
}

func (a *fakeAdmission) Admit(context.Context, *repository.AuthIdentity, *provider.ResponseRequest) error {
	a.calls++
	return a.err
}

type fakeCache struct {
	entry  *cache.Entry
	hit    bool
	writes int
}

func (c *fakeCache) Lookup(context.Context, *repository.AuthIdentity, *provider.ResponseRequest) (*cache.Entry, bool, error) {
	return c.entry, c.hit, nil
}
func (c *fakeCache) Store(context.Context, *repository.AuthIdentity, *provider.ResponseRequest, *cache.Entry) error {
	c.writes++
	return nil
}

type fakeRepo struct{ created, completed, failed int }

func (r *fakeRepo) Create(context.Context, repository.ResponseRecord) error { r.created++; return nil }
func (r *fakeRepo) Complete(context.Context, repository.ResponseRecord) error {
	r.completed++
	return nil
}
func (r *fakeRepo) Fail(context.Context, repository.ResponseRecord) error { r.failed++; return nil }

type fakeUsage struct{ records int }

func (u *fakeUsage) Record(context.Context, *repository.AuthIdentity, string, *provider.Response, time.Duration, error) error {
	u.records++
	return nil
}

func TestOrchestratorCreateRetriesAndFallsBack(t *testing.T) {
	first := &fakeProvider{name: "first", err: errors.New("temporary")}
	second := &fakeProvider{name: "second", resp: &provider.Response{Model: "m", Usage: provider.Usage{TotalTokens: 2}}}
	repo, usage := &fakeRepo{}, &fakeUsage{}
	o := NewOrchestrator(Dependencies{Admission: &fakeAdmission{}, Router: fakeRouter{[]provider.Provider{first, second}}, Repository: repo, Usage: usage, Retry: RetryPolicy{MaxAttempts: 2}})
	result, err := o.Create(context.Background(), &repository.AuthIdentity{TenantID: "t"}, &provider.ResponseRequest{Model: "m"}, "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.ProviderName != "second" || first.calls != 2 {
		t.Fatalf("fallback/retry = provider %q calls %d", result.ProviderName, first.calls)
	}
	if repo.created != 1 || repo.completed != 1 || usage.records != 1 {
		t.Fatalf("persistence counts: created=%d completed=%d usage=%d", repo.created, repo.completed, usage.records)
	}
}

func TestOrchestratorCreateUsesCache(t *testing.T) {
	body := []byte(`{"model":"m"}`)
	cachePort := &fakeCache{hit: true, entry: &cache.Entry{Response: body, Provider: "cached"}}
	invoked := &fakeProvider{name: "provider", resp: &provider.Response{Model: "m"}}
	o := NewOrchestrator(Dependencies{Admission: &fakeAdmission{}, Router: fakeRouter{[]provider.Provider{invoked}}, Cache: cachePort, Repository: &fakeRepo{}})
	result, err := o.Create(context.Background(), &repository.AuthIdentity{TenantID: "t"}, &provider.ResponseRequest{Model: "m"}, "")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.ProviderName != "cached" || invoked.calls != 0 {
		t.Fatalf("cache result = %#v, provider calls=%d", result, invoked.calls)
	}
}

func TestOrchestratorStreamDrainsAfterDisconnect(t *testing.T) {
	p := &fakeProvider{name: "stream"}
	stream := make(chan provider.ResponseEvent, 1)
	errCh := make(chan error, 1)
	stream <- provider.ResponseEvent{Type: provider.EventContentDelta, Delta: "hello", Usage: &provider.Usage{TotalTokens: 1}}
	invoker := streamInvoker{events: stream, errs: errCh}
	o := NewOrchestrator(Dependencies{Admission: &fakeAdmission{}, Router: fakeRouter{[]provider.Provider{p}}, Invoker: invoker, Repository: &fakeRepo{}, DrainTimeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	result, err := o.CreateStream(ctx, &repository.AuthIdentity{TenantID: "t"}, &provider.ResponseRequest{Model: "m", Stream: true}, "")
	if err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	<-result.Events
	cancel()
	stream <- provider.ResponseEvent{Type: provider.EventResponseCompleted, Response: &provider.Response{Model: "m", Usage: provider.Usage{TotalTokens: 1}}}
	close(stream)
	close(errCh)
	for range result.Events {
	}
	if _, ok := <-result.Errors; ok {
	}
	if !result.Drained {
		t.Fatal("stream was not drained after disconnect")
	}
}

type streamInvoker struct {
	events <-chan provider.ResponseEvent
	errs   <-chan error
}

func (i streamInvoker) Invoke(context.Context, provider.Provider, *provider.ResponseRequest) (*provider.Response, error) {
	return nil, nil
}
func (i streamInvoker) Stream(context.Context, provider.Provider, *provider.ResponseRequest) (<-chan provider.ResponseEvent, <-chan error) {
	return i.events, i.errs
}
