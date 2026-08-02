package platform

import (
	"errors"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
)

func TestBuildSyncPlanMergesResources(t *testing.T) {
	stream := true
	replicas := 2
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
				TargetRef:   TargetRef{Kind: "InferenceService", Name: "qwen"},
				Mode:        "enforce",
				MaxReplicas: 3,
				Metrics: AutoscaleMetricTargets{
					QueueDepth: 10,
				},
				Behavior: AutoscaleBehavior{MaxScaleUpStep: 1},
			},
		}},
		InferenceServices: []InferenceService{{
			Metadata: ObjectMeta{Name: "qwen", Namespace: "llm"},
			Spec: InferenceServiceSpec{
				Runtime:  "vllm",
				Model:    "Qwen/Qwen3",
				Image:    "registry.local/qwen:v1",
				Replicas: &replicas,
				Serving: InferenceServingSpec{
					Port:        8000,
					OpenAIPath:  "/v1",
					MetricsPath: "/metrics",
				},
				AutoscalePolicyRef: &TargetRef{Name: "scale-qwen"},
				RouteLabels:        map[string]string{"accelerator": "h100"},
			},
		}},
		RuntimeSignals: map[string]RuntimeSignals{
			"llm/qwen": {QueueDepth: 12},
		},
	}

	plan, err := BuildSyncPlan(snapshot, "llm")
	if err != nil {
		t.Fatalf("BuildSyncPlan: %v", err)
	}
	if len(plan.Providers) != 2 || plan.Providers[0].Provider.BaseURL != "http://qwen-svc.llm.svc:8000/v1" {
		t.Fatalf("providers = %#v, want explicit and exposed inference service providers", plan.Providers)
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
	if len(plan.Workloads.Deployments) != 1 || plan.Workloads.Deployments[0].Replicas != 3 {
		t.Fatalf("workload deployments = %#v, want autoscaled deployment", plan.Workloads.Deployments)
	}
	if len(plan.Workloads.Services) != 1 || plan.Workloads.Services[0].Port != 8000 {
		t.Fatalf("workload services = %#v, want service port", plan.Workloads.Services)
	}
	if plan.Providers[1].Provider.BaseURL != "http://qwen.llm.svc:8000/v1" {
		t.Fatalf("providers = %#v, want exposed inference service provider", plan.Providers)
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

func TestBuildSyncPlanAutoscaleWithoutSignalsClampsOnly(t *testing.T) {
	replicas := 2
	plan, err := BuildSyncPlan(ResourceSnapshot{
		InferenceServices: []InferenceService{{
			Metadata: ObjectMeta{Name: "qwen", Namespace: "llm"},
			Spec: InferenceServiceSpec{
				Runtime:  "vllm",
				Model:    "Qwen/Qwen3",
				Replicas: &replicas,
			},
		}},
		AutoscalePolicies: []InferenceAutoscalePolicy{{
			Metadata: ObjectMeta{Name: "scale-qwen"},
			Spec: InferenceAutoscalePolicySpec{
				TargetRef:   TargetRef{Kind: "InferenceService", Name: "qwen"},
				Mode:        "enforce",
				MinReplicas: 1,
				MaxReplicas: 5,
				Metrics:     AutoscaleMetricTargets{QueueDepth: 10},
			},
		}},
	}, "llm")
	if err != nil {
		t.Fatalf("BuildSyncPlan: %v", err)
	}
	if got := plan.Workloads.Deployments[0].Replicas; got != 2 {
		t.Fatalf("replicas = %d, want unchanged without runtime signals", got)
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
