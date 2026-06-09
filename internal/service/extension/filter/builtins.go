package filter

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/limiter"
)

// ModelWhitelistFilter checks whether the requested model is allowed
// for the authenticated identity.
type ModelWhitelistFilter struct {
	auth *auth.Auth
}

// NewModelWhitelistFilter creates a ModelWhitelistFilter.
func NewModelWhitelistFilter(authSvc *auth.Auth) *ModelWhitelistFilter {
	return &ModelWhitelistFilter{auth: authSvc}
}

func (f *ModelWhitelistFilter) Name() string { return "model_whitelist" }

func (f *ModelWhitelistFilter) Process(req *RequestContext) Result {
	if f.auth == nil || req.Identity == nil {
		return Result{Action: Allow}
	}
	if !f.auth.CheckModel(req.Identity, req.Model) {
		return Result{
			Action:        Block,
			Error:         auth.ErrModelNotAllowed,
			HTTPStatus:    http.StatusForbidden,
			ErrorType:     "invalid_request_error",
			MetricsResult: "auth_error",
			MetricsClass:  "model_not_allowed",
		}
	}
	return Result{Action: Allow}
}

// QuotaFilter checks whether the identity has sufficient quota for the request.
type QuotaFilter struct {
	auth *auth.Auth
}

// NewQuotaFilter creates a QuotaFilter.
func NewQuotaFilter(authSvc *auth.Auth) *QuotaFilter {
	return &QuotaFilter{auth: authSvc}
}

func (f *QuotaFilter) Name() string { return "quota" }

func (f *QuotaFilter) Process(req *RequestContext) Result {
	if f.auth == nil || req.Identity == nil {
		return Result{Action: Allow}
	}
	if !f.auth.HasQuota(req.Identity, req.EstimatedTokens) {
		return Result{
			Action:        Block,
			Error:         auth.ErrQuotaExceeded,
			HTTPStatus:    http.StatusTooManyRequests,
			ErrorType:     "rate_limit_error",
			MetricsResult: "rate_limited",
			MetricsClass:  "quota_exceeded",
		}
	}
	return Result{Action: Allow}
}

// BudgetNotifier is the minimal interface BudgetFilter needs from the alert service.
type BudgetNotifier interface {
	NotifyBudgetExhausted(ctx context.Context, event alert.BudgetExhausted)
}

// BudgetFilter performs a pre-flight budget check across the budget hierarchy
// (virtual key → API key → project → tenant).
type BudgetFilter struct {
	budgetSvc *budget.Service
	alertSvc  BudgetNotifier
}

// NewBudgetFilter creates a BudgetFilter.
func NewBudgetFilter(budgetSvc *budget.Service, alertSvc BudgetNotifier) *BudgetFilter {
	return &BudgetFilter{budgetSvc: budgetSvc, alertSvc: alertSvc}
}

func (f *BudgetFilter) Name() string { return "budget" }

func (f *BudgetFilter) Process(req *RequestContext) Result {
	if f.budgetSvc == nil || req.Identity == nil {
		return Result{Action: Allow}
	}
	result, err := f.budgetSvc.Check(req.Context, budget.CheckRequest{
		Identity:      req.Identity,
		EstimatedCost: 0,
		ProviderName:  "",
		Model:         req.Model,
	})
	if err != nil {
		return Result{
			Action:        Block,
			Error:         fmt.Errorf("budget check failed: %w", err),
			HTTPStatus:    http.StatusInternalServerError,
			ErrorType:     "internal_error",
			MetricsResult: "client_error",
			MetricsClass:  "budget_check_error",
		}
	}
	if !result.Allowed {
		rejectErr := result.RejectError
		if rejectErr == nil {
			rejectErr = errors.New("budget exceeded")
		}
		return Result{
			Action:        Block,
			Error:         rejectErr,
			HTTPStatus:    http.StatusTooManyRequests,
			ErrorType:     "rate_limit_error",
			MetricsResult: "rate_limited",
			MetricsClass:  "budget_exceeded",
		}
	}
	// Soft-alert side effect: notify when a budget scope is near exhaustion.
	if result.AlertSent && f.alertSvc != nil {
		scope := firstSoftAlertScope(result.Scopes)
		f.alertSvc.NotifyBudgetExhausted(req.Context, alert.BudgetExhausted{
			TenantID:    req.Identity.TenantID,
			ProjectID:   req.Identity.ProjectID,
			APIKeyID:    req.Identity.APIKeyID,
			Model:       req.Model,
			BudgetScope: scope,
		})
	}
	return Result{Action: Allow}
}

func firstSoftAlertScope(scopes []budget.ScopeResult) string {
	for _, s := range scopes {
		if s.Policy == repository.BudgetPolicySoftAlert {
			return s.Scope
		}
	}
	return "unknown"
}

// RateLimitFilter enforces rate limits across multiple dimensions:
//   - per-API-key QPS (via limiter.Allow)
//   - tenant-level TPM/RPM (via limiter.CheckTenant)
//   - model-level TPM/RPM (via limiter.CheckModel)
type RateLimitFilter struct {
	auth    *auth.Auth
	limiter *limiter.Limiter
}

// NewRateLimitFilter creates a RateLimitFilter.
func NewRateLimitFilter(authSvc *auth.Auth, limiterSvc *limiter.Limiter) *RateLimitFilter {
	return &RateLimitFilter{auth: authSvc, limiter: limiterSvc}
}

func (f *RateLimitFilter) Name() string { return "rate_limit" }

func (f *RateLimitFilter) Process(req *RequestContext) Result {
	if f.limiter == nil || req.Identity == nil {
		return Result{Action: Allow}
	}

	// Per-user QPS + token bucket
	qps := 0
	if f.auth != nil {
		qps = f.auth.EffectiveRateLimitQPS(req.Identity)
	}
	if !f.limiter.Allow(req.Context, req.Identity.APIKey, qps, req.EstimatedTokens) {
		return Result{
			Action:        Block,
			Error:         errors.New("rate limit exceeded"),
			HTTPStatus:    http.StatusTooManyRequests,
			ErrorType:     "rate_limit_error",
			MetricsResult: "rate_limited",
			MetricsClass:  "rate_limited",
		}
	}

	// Tenant-level limit
	if !f.limiter.CheckTenant(req.Identity.TenantID, req.EstimatedTokens) {
		return Result{
			Action:        Block,
			Error:         errors.New("tenant rate limit exceeded"),
			HTTPStatus:    http.StatusTooManyRequests,
			ErrorType:     "rate_limit_error",
			MetricsResult: "rate_limited",
			MetricsClass:  "tenant_rate_limited",
		}
	}

	// Model-level limit
	if !f.limiter.CheckModel(req.Model, req.EstimatedTokens) {
		return Result{
			Action:        Block,
			Error:         errors.New("model rate limit exceeded"),
			HTTPStatus:    http.StatusTooManyRequests,
			ErrorType:     "rate_limit_error",
			MetricsResult: "rate_limited",
			MetricsClass:  "model_rate_limited",
		}
	}

	return Result{Action: Allow}
}
