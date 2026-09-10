package responses

import (
	"context"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/limiter"
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

	if s.auth != nil {
		if !s.auth.CheckModel(identity, req.Model) {
			return auth.ErrModelNotAllowed
		}
	}

	return nil
}

func (s *Service) checkRateLimit(ctx context.Context, identity *repository.AuthIdentity, req *provider.ResponseRequest, providerName string) limiter.Decision {
	if s.limiter == nil || identity == nil || req == nil {
		return limiter.Decision{Allowed: true}
	}
	return s.limiter.Check(ctx, limiter.LimitRequest{
		TenantID: identity.TenantID,
		Provider: providerName,
		Model:    req.Model,
		Tokens:   req.EstimateAdmissionTokens(),
	})
}

func (s *Service) markRateLimitResponse(ctx context.Context, identity *repository.AuthIdentity, responseID, providerName, model string, trace *routeTrace) {
	if s.store == nil || identity == nil || responseID == "" {
		return
	}
	_ = s.store.UpdateResponse(ctx, repository.ResponseRecord{
		ID:             responseID,
		TenantID:       identity.TenantID,
		ProjectID:      identity.ProjectID,
		ProviderName:   providerName,
		Model:          model,
		Status:         "error",
		RouteTraceBody: routeTraceBytes(trace),
	})
}
