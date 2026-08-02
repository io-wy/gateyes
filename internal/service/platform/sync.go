package platform

import (
	"errors"
	"fmt"
	"sort"

	"github.com/gateyes/gateway/internal/app/config"
)

type ResourceSnapshot struct {
	ModelEndpoints    []ModelEndpoint
	RoutePolicies     []RoutePolicy
	BudgetPolicies    []BudgetPolicy
	AutoscalePolicies []InferenceAutoscalePolicy
}

type SyncPlan struct {
	Providers         []ProviderSyncTarget
	Router            config.RouterConfig
	Budgets           []BudgetSyncTarget
	AutoscalePolicies []InferenceAutoscalePolicy
}

type AdminSyncClient interface {
	SyncProvider(target ProviderSyncTarget) error
	SyncRouter(router config.RouterConfig) error
	SyncBudget(target BudgetSyncTarget) error
}

func BuildSyncPlan(snapshot ResourceSnapshot, defaultNamespace string) (SyncPlan, error) {
	var plan SyncPlan
	var errs []error

	for _, endpoint := range snapshot.ModelEndpoints {
		target, err := endpoint.ToProviderSync(defaultNamespace)
		if err != nil {
			errs = append(errs, fmt.Errorf("model endpoint %s: %w", endpoint.Metadata.Name, err))
			continue
		}
		plan.Providers = append(plan.Providers, target)
	}

	routerCfg, err := buildRouterConfig(snapshot.RoutePolicies)
	if err != nil {
		errs = append(errs, err)
	} else {
		plan.Router = routerCfg
	}

	for _, policy := range snapshot.BudgetPolicies {
		target, err := policy.ToBudgetSync()
		if err != nil {
			errs = append(errs, fmt.Errorf("budget policy %s: %w", policy.Metadata.Name, err))
			continue
		}
		plan.Budgets = append(plan.Budgets, target)
	}

	plan.AutoscalePolicies = append(plan.AutoscalePolicies, snapshot.AutoscalePolicies...)
	return plan, errors.Join(errs...)
}

func ApplySyncPlan(plan SyncPlan, client AdminSyncClient) error {
	var errs []error
	for _, providerTarget := range plan.Providers {
		if err := client.SyncProvider(providerTarget); err != nil {
			errs = append(errs, fmt.Errorf("sync provider %s: %w", providerTarget.Provider.Name, err))
		}
	}
	if plan.Router.Strategy != "" || plan.Router.RuleEngine.Enabled {
		if err := client.SyncRouter(plan.Router); err != nil {
			errs = append(errs, fmt.Errorf("sync router: %w", err))
		}
	}
	for _, budgetTarget := range plan.Budgets {
		if err := client.SyncBudget(budgetTarget); err != nil {
			errs = append(errs, fmt.Errorf("sync budget %s/%s: %w", budgetTarget.SubjectKind, budgetTarget.SubjectName, err))
		}
	}
	return errors.Join(errs...)
}

func buildRouterConfig(policies []RoutePolicy) (config.RouterConfig, error) {
	if len(policies) == 0 {
		return config.RouterConfig{}, nil
	}
	ordered := append([]RoutePolicy{}, policies...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Spec.Priority > ordered[j].Spec.Priority
	})

	var routerCfg config.RouterConfig
	for i, policy := range ordered {
		cfg := policy.ToRouterConfig()
		if i == 0 {
			routerCfg.Strategy = cfg.Strategy
		}
		if cfg.RuleEngine.Enabled {
			routerCfg.RuleEngine.Enabled = true
			routerCfg.RuleEngine.Rules = append(routerCfg.RuleEngine.Rules, cfg.RuleEngine.Rules...)
		}
	}
	return routerCfg, nil
}
