package inference

import (
	"context"
	"time"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/provider"
)

// StreamCache is the cache boundary shared by unary and streaming flows.
// Stream entries may contain a transcript; callers must not mutate entries
// returned by Lookup.
type StreamCache interface {
	Lookup(context.Context, *repository.AuthIdentity, *provider.ResponseRequest) (*cache.Entry, bool, error)
	Store(context.Context, *repository.AuthIdentity, *provider.ResponseRequest, *cache.Entry) error
}

type PluginHooks interface {
	Before(context.Context, *provider.ResponseRequest) (*provider.ResponseRequest, error)
	After(context.Context, *provider.Response) (*provider.Response, error)
	Audit(context.Context, *provider.Response)
}

// DrainConfig documents the bounded cancellation behavior for stream ports.
type DrainConfig struct{ Timeout time.Duration }
