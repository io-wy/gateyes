package platform

import (
	"errors"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
)

func TestBuildSyncPlanMergesResources(t *testing.T) {
	stream := true
	snapshot := ResourceSnapshot{
		ModelEndpoints: []ModelEndpoint{{
			Metadata: ObjectMeta{Name: "qwen"},
			Spec: ModelEndpointSpec{
				Type:       "vllm",
				ServiceRef: &ServiceRef{Name: "qwen-svc", Port: 8000},
				Model:      "Qwen/Qwen3",
				Capabilities: CapabilitySpec{
					Stream: &stream,
				},
			},
		}},
		RoutePolicies: []RoutePolicy{
			{
				Metadata: ObjectMeta{Name: "low"},
				Spec: RoutePolicySpec{
					Priority:   1,
					Strategy:   "cost_based",
					TargetRefs: []TargetRef{{Kind: "ModelEndpoint", Name: "cheap"}},
				},
			},
			{
				Metadata: ObjectMeta{Name: "high"},
				Spec: RoutePolicySpec{
					Priority:   10,
					Strategy:   "least_gpu_cache",
					TargetRefs: []TargetRef{{Kind: "ModelEndpoint", Name: "fast"}},
				},
			},
		},
		BudgetPolicies: []BudgetPolicy{{
			Metadata: ObjectMeta{Name: "tenant-budget"},
			Spec: BudgetPolicySpec{
				Subject:     BudgetSubject{Kind: "tenant", Name: "platform"},
				Limits:      BudgetLimits{MonthlyCost: 100},
				Enforcement: "hard_reject",
			},
		}},
		AutoscalePolicies: []InferenceAutoscalePolicy{{
			Metadata: ObjectMeta{Name: "scale-qwen"},
			Spec: InferenceAutoscalePolicySpec{
				TargetRef: TargetRef{Kind: "InferenceService", Name: "qwen"},
			},
		}},
	}

	plan, err := BuildSyncPlan(snapshot, "llm")
	if err != nil {
		t.Fatalf("BuildSyncPlan: %v", err)
	}
	if len(plan.Providers) != 1 || plan.Providers[0].Provider.BaseURL != "http://qwen-svc.llm.svc:8000/v1" {
		t.Fatalf("providers = %#v", plan.Providers)
	}
	if plan.Router.Strategy != "least_gpu_cache" {
		t.Fatalf("router strategy = %q", plan.Router.Strategy)
	}
	if len(plan.Router.RuleEngine.Rules) != 2 || plan.Router.RuleEngine.Rules[0].Name != "high" {
		t.Fatalf("router rules = %#v", plan.Router.RuleEngine.Rules)
	}
	if len(plan.Budgets) != 1 || plan.Budgets[0].BudgetUSD != 100 {
		t.Fatalf("budgets = %#v", plan.Budgets)
	}
	if len(plan.AutoscalePolicies) != 1 {
		t.Fatalf("autoscale policies = %#v", plan.AutoscalePolicies)
	}
}

func TestApplySyncPlanCallsClient(t *testing.T) {
	client := &recordingSyncClient{}
	plan := SyncPlan{
		Providers: []ProviderSyncTarget{{Provider: config.ProviderConfig{Name: "p1"}}},
		Router: RoutePolicy{
			Metadata: ObjectMeta{Name: "route"},
			Spec:     RoutePolicySpec{TargetRefs: []TargetRef{{Kind: "ModelEndpoint", Name: "p1"}}},
		}.ToRouterConfig(),
		Budgets: []BudgetSyncTarget{{SubjectKind: "tenant", SubjectName: "platform"}},
	}

	if err := ApplySyncPlan(plan, client); err != nil {
		t.Fatalf("ApplySyncPlan: %v", err)
	}
	if client.providers != 1 || client.routers != 1 || client.budgets != 1 {
		t.Fatalf("client counts = %+v", client)
	}
}

func TestBuildSyncPlanReturnsJoinedErrors(t *testing.T) {
	_, err := BuildSyncPlan(ResourceSnapshot{
		ModelEndpoints: []ModelEndpoint{{Metadata: ObjectMeta{Name: "bad-endpoint"}}},
		BudgetPolicies: []BudgetPolicy{{
			Metadata: ObjectMeta{Name: "bad-budget"},
		}},
	}, "default")
	if err == nil {
		t.Fatal("BuildSyncPlan error = nil, want joined errors")
	}
	if !errors.Is(err, ErrMissingModelEndpointTarget) || !errors.Is(err, ErrMissingBudgetSubject) {
		t.Fatalf("BuildSyncPlan error = %v, want endpoint and budget errors", err)
	}
}

type recordingSyncClient struct {
	providers int
	routers   int
	budgets   int
}

func (c *recordingSyncClient) SyncProvider(ProviderSyncTarget) error {
	c.providers++
	return nil
}

func (c *recordingSyncClient) SyncRouter(config.RouterConfig) error {
	c.routers++
	return nil
}

func (c *recordingSyncClient) SyncBudget(BudgetSyncTarget) error {
	c.budgets++
	return nil
}
