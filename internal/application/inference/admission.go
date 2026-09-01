package inference

import (
	"context"
	"time"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

// AdmissionPolicy is the application port for authentication, model policy,
// rate limiting and budget pre-checks. Admission is intentionally performed
// before cache lookup so a cached response cannot bypass tenant policy.
type AdmissionPolicy interface {
	Admit(context.Context, *repository.AuthIdentity, *provider.ResponseRequest) error
}

// AdmissionFunc makes small adapters and tests straightforward.
type AdmissionFunc func(context.Context, *repository.AuthIdentity, *provider.ResponseRequest) error

func (f AdmissionFunc) Admit(ctx context.Context, id *repository.AuthIdentity, req *provider.ResponseRequest) error {
	return f(ctx, id, req)
}

// RetryPolicy controls retry delay without coupling application code to a
// concrete configuration package.
type RetryPolicy struct {
	MaxAttempts  int
	InitialDelay time.Duration
}
