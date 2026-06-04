package filter

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/auth"
)

// alwaysAllow is a test filter that always allows.
type alwaysAllow struct{ name string }

func (f *alwaysAllow) Name() string { return f.name }
func (f *alwaysAllow) Process(req *RequestContext) Result {
	return Result{Action: Allow}
}

// alwaysBlock is a test filter that always blocks.
type alwaysBlock struct {
	name   string
	status int
}

func (f *alwaysBlock) Name() string { return f.name }
func (f *alwaysBlock) Process(req *RequestContext) Result {
	return Result{
		Action:     Block,
		Error:      errors.New(f.name + " blocked"),
		HTTPStatus: f.status,
	}
}

// transformFilter mutates the request model.
type transformFilter struct{ model string }

func (f *transformFilter) Name() string { return "transform" }
func (f *transformFilter) Process(req *RequestContext) Result {
	req.Model = f.model
	return Result{Action: Allow}
}

func TestPipelineExecute_Allow(t *testing.T) {
	p := NewPipeline([]Filter{
		&alwaysAllow{name: "a"},
		&alwaysAllow{name: "b"},
	})
	res := p.Execute(&RequestContext{Context: context.Background()})
	if res.Action != Allow {
		t.Fatalf("expected Allow, got %v", res.Action)
	}
}

func TestPipelineExecute_Block(t *testing.T) {
	p := NewPipeline([]Filter{
		&alwaysAllow{name: "a"},
		&alwaysBlock{name: "b", status: http.StatusTooManyRequests},
		&alwaysAllow{name: "c"},
	})
	res := p.Execute(&RequestContext{Context: context.Background()})
	if res.Action != Block {
		t.Fatalf("expected Block, got %v", res.Action)
	}
	if res.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d", http.StatusTooManyRequests, res.HTTPStatus)
	}
	if res.Error == nil || res.Error.Error() != "b blocked" {
		t.Fatalf("unexpected error: %v", res.Error)
	}
}

func TestPipelineExecute_EmptyChain(t *testing.T) {
	p := NewPipeline(nil)
	res := p.Execute(&RequestContext{Context: context.Background()})
	if res.Action != Allow {
		t.Fatalf("expected Allow for empty chain, got %v", res.Action)
	}
}

func TestPipelineExecute_Transform(t *testing.T) {
	p := NewPipeline([]Filter{
		&transformFilter{model: "gpt-4"},
	})
	req := &RequestContext{Context: context.Background(), Model: "gpt-3"}
	p.Execute(req)
	if req.Model != "gpt-4" {
		t.Fatalf("expected model mutated to gpt-4, got %s", req.Model)
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	f := func() (Filter, error) { return &alwaysAllow{name: "test"}, nil }

	if err := r.Register("test", f); err != nil {
		t.Fatalf("unexpected register error: %v", err)
	}

	factory, ok := r.Get("test")
	if !ok {
		t.Fatal("expected factory to exist")
	}
	filter, err := factory()
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
	if filter.Name() != "test" {
		t.Fatalf("expected name test, got %s", filter.Name())
	}
}

func TestRegistry_Duplicate(t *testing.T) {
	r := NewRegistry()
	f := func() (Filter, error) { return &alwaysAllow{name: "x"}, nil }
	if err := r.Register("dup", f); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := r.Register("dup", f); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestRegistry_BuildPipeline(t *testing.T) {
	r := NewRegistry()
	r.Register("allow", func() (Filter, error) { return &alwaysAllow{name: "allow"}, nil })

	p, err := r.BuildPipeline([]string{"allow"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	res := p.Execute(&RequestContext{Context: context.Background()})
	if res.Action != Allow {
		t.Fatalf("expected Allow, got %v", res.Action)
	}
}

func TestRegistry_BuildPipeline_Unknown(t *testing.T) {
	r := NewRegistry()
	_, err := r.BuildPipeline([]string{"missing"})
	if err == nil {
		t.Fatal("expected error for unknown filter")
	}
}

// ModelWhitelistFilter tests

func TestModelWhitelistFilter_Allowed(t *testing.T) {
	authSvc := auth.NewAuth(nil)
	f := NewModelWhitelistFilter(authSvc)
	res := f.Process(&RequestContext{
		Context:  context.Background(),
		Identity: &repository.AuthIdentity{},
		Model:    "gpt-4",
	})
	if res.Action != Allow {
		t.Fatalf("expected Allow when no model restrictions, got %v", res.Action)
	}
}

func TestModelWhitelistFilter_Blocked(t *testing.T) {
	authSvc := auth.NewAuth(nil)
	f := NewModelWhitelistFilter(authSvc)
	res := f.Process(&RequestContext{
		Context: context.Background(),
		Identity: &repository.AuthIdentity{
			APIKeyModels: []string{"gpt-3"},
		},
		Model: "gpt-4",
	})
	if res.Action != Block {
		t.Fatalf("expected Block, got %v", res.Action)
	}
	if res.HTTPStatus != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", res.HTTPStatus)
	}
	if !errors.Is(res.Error, auth.ErrModelNotAllowed) {
		t.Fatalf("expected ErrModelNotAllowed, got %v", res.Error)
	}
}

// QuotaFilter tests

func TestQuotaFilter_NoQuota(t *testing.T) {
	authSvc := auth.NewAuth(nil)
	f := NewQuotaFilter(authSvc)
	res := f.Process(&RequestContext{
		Context:  context.Background(),
		Identity: &repository.AuthIdentity{Quota: 0},
		Model:    "gpt-4",
	})
	if res.Action != Allow {
		t.Fatalf("expected Allow when quota is unlimited, got %v", res.Action)
	}
}

func TestQuotaFilter_Exceeded(t *testing.T) {
	authSvc := auth.NewAuth(nil)
	f := NewQuotaFilter(authSvc)
	res := f.Process(&RequestContext{
		Context:  context.Background(),
		Identity: &repository.AuthIdentity{Quota: 100, Used: 50},
		Model:    "gpt-4",
		EstimatedTokens: 100,
	})
	if res.Action != Block {
		t.Fatalf("expected Block, got %v", res.Action)
	}
	if res.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", res.HTTPStatus)
	}
}

// RateLimitFilter tests with nil limiter

func TestRateLimitFilter_NilLimiter(t *testing.T) {
	authSvc := auth.NewAuth(nil)
	f := NewRateLimitFilter(authSvc, nil)
	res := f.Process(&RequestContext{
		Context:  context.Background(),
		Identity: &repository.AuthIdentity{},
	})
	if res.Action != Allow {
		t.Fatalf("expected Allow when limiter is nil, got %v", res.Action)
	}
}
