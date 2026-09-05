package inference

import (
	"context"
	"time"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

// ProviderRouter selects an ordered list. The order is significant: the
// orchestrator retries a provider before moving to the next fallback.
type ProviderRouter interface {
	Plan(context.Context, *repository.AuthIdentity, *provider.ResponseRequest, string) ([]provider.Provider, error)
}

// ProviderInvoker isolates SDK/provider calls from workflow decisions.
type ProviderInvoker interface {
	Invoke(context.Context, provider.Provider, *provider.ResponseRequest) (*provider.Response, error)
	Stream(context.Context, provider.Provider, *provider.ResponseRequest) (<-chan provider.ResponseEvent, <-chan error)
}

// UsageRecorder owns billing and token accounting. Implementations should be
// idempotent because a stream can terminate through either completion or
// cancellation paths.
type UsageRecorder interface {
	Record(context.Context, *repository.AuthIdentity, string, *provider.Response, time.Duration, error) error
}

// ResponseRepository is the persistence boundary for response lifecycle
// transitions. The orchestrator never reaches into a concrete SQL store.
type ResponseRepository interface {
	Create(context.Context, repository.ResponseRecord) error
	Complete(context.Context, repository.ResponseRecord) error
	Fail(context.Context, repository.ResponseRecord) error
}
