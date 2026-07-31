package responses

import (
	"context"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/provider"
)

type admissionCheckedKey struct{}

// WithAdmissionChecked marks ctx after an upstream caller has already run the
// request admission checks. It prevents double-consuming token buckets when
// catalog services delegate into the generic responses runtime.
func WithAdmissionChecked(ctx context.Context) context.Context {
	return context.WithValue(ctx, admissionCheckedKey{}, true)
}

func admissionChecked(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	checked, _ := ctx.Value(admissionCheckedKey{}).(bool)
	return checked
}

func (s *Service) admitRequest(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest) error {
	if admissionChecked(ctx) || identity == nil || req == nil {
		return nil
	}

	tokens := req.EstimateAdmissionTokens()
	if s.auth != nil {
		if !s.auth.CheckModel(identity, req.Model) {
			return auth.ErrModelNotAllowed
		}
	}

	if s.limiter == nil || s.auth == nil {
		return nil
	}
	if !s.limiter.Allow(ctx, identity.APIKey, s.auth.EffectiveRateLimitQPS(identity), tokens) {
		return ErrRateLimited
	}
	if !s.limiter.CheckTenant(identity.TenantID, tokens) {
		return ErrRateLimited
	}
	if !s.limiter.CheckModel(req.Model, tokens) {
		return ErrRateLimited
	}
	return nil
}
