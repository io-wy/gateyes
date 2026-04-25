package budget

import (
	"context"
	"errors"
	"fmt"

	"github.com/gateyes/gateway/internal/repository"
)

var (
	ErrBudgetHardRejected = errors.New("budget exhausted: request blocked")
	ErrBudgetSoftAlert    = errors.New("budget exhausted: alert sent")
)

type BudgetStore interface {
	CheckAPIKeyBudget(ctx context.Context, apiKeyID string, estimatedCost float64) (*repository.BudgetCheckResult, error)
	CheckProjectBudget(ctx context.Context, projectID string, estimatedCost float64) (*repository.BudgetCheckResult, error)
	CheckTenantBudget(ctx context.Context, tenantID string, estimatedCost float64) (*repository.BudgetCheckResult, error)
}

type Service struct {
	store BudgetStore
}

func New(store BudgetStore) *Service {
	return &Service{store: store}
}

type CheckRequest struct {
	Identity      *repository.AuthIdentity
	EstimatedCost float64
	ProviderName  string
	Model         string
}

type CheckResult struct {
	Allowed     bool
	RejectError error
	AlertSent   bool
	Scopes      []ScopeResult
}

type ScopeResult struct {
	Scope     string
	Policy    string
	Allowed   bool
	Remaining float64
}

func (s *Service) Check(ctx context.Context, req CheckRequest) (CheckResult, error) {
	result := CheckResult{Allowed: true, Scopes: make([]ScopeResult, 0, 3)}

	// Check API Key -> Project -> Tenant in order
	scopes := []struct {
		checkFunc func(context.Context, string, float64) (*repository.BudgetCheckResult, error)
		id        string
		name      string
		policy    string
	}{
		{s.store.CheckAPIKeyBudget, req.Identity.APIKeyID, "api_key", req.Identity.APIKeyBudgetPolicy},
		{s.store.CheckProjectBudget, req.Identity.ProjectID, "project", req.Identity.ProjectBudgetPolicy},
		{s.store.CheckTenantBudget, req.Identity.TenantID, "tenant", req.Identity.TenantBudgetPolicy},
	}

	for _, scope := range scopes {
		if scope.id == "" && scope.name != "tenant" {
			continue
		}
		checkResult, err := scope.checkFunc(ctx, scope.id, req.EstimatedCost)
		if err != nil {
			return CheckResult{}, fmt.Errorf("check %s budget: %w", scope.name, err)
		}

		// Use stored policy if check didn't return one
		if checkResult.Policy == "" {
			checkResult.Policy = scope.policy
		}

		sr := ScopeResult{
			Scope:     scope.name,
			Policy:    checkResult.Policy,
			Allowed:   checkResult.Allowed,
			Remaining: checkResult.Remaining,
		}
		result.Scopes = append(result.Scopes, sr)

		if !checkResult.Allowed {
			switch checkResult.Policy {
			case repository.BudgetPolicyHardReject:
				result.Allowed = false
				result.RejectError = ErrBudgetHardRejected
				return result, nil
			case repository.BudgetPolicySoftAlert:
				result.AlertSent = true
				// allow but mark alert
			case repository.BudgetPolicyGrace:
				// allow but track overage in post-usage
			default:
				result.Allowed = false
				result.RejectError = ErrBudgetHardRejected
				return result, nil
			}
		}
	}

	return result, nil
}
