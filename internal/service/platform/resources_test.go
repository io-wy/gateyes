package platform

import (
	"errors"
	"testing"
)

func TestModelEndpointToProviderSyncResolvesSelfHostedRuntime(t *testing.T) {
	stream := true
	endpoint := ModelEndpoint{
		Metadata: ObjectMeta{Name: "qwen-vllm"},
		Spec: ModelEndpointSpec{
			Type:     "vllm",
			Endpoint: "chat",
			ServiceRef: &ServiceRef{
				Name: "qwen-router",
				Port: 8000,
				Path: "/v1",
			},
			APIKeySecretRef: &SecretKeyRef{Name: "qwen-secret", Key: "api-key"},
			Model:           "Qwen/Qwen3-32B",
			ModelAliases: map[string]string{
				"qwen-large": "Qwen/Qwen3-32B",
			},
			Weight: 50,
			Metrics: MetricsSpec{
				URL: "http://qwen-router.llm.svc:8000/metrics",
			},
			Pricing: PricingSpec{Input: 0.000001, Output: 0.000003},
			Capabilities: CapabilitySpec{
				Chat:   &stream,
				Stream: &stream,
			},
			RouteLabels: map[string]string{"runtime": "vllm", "accelerator": "h100"},
		},
	}

	syncTarget, err := endpoint.ToProviderSync("llm")
	if err != nil {
		t.Fatalf("ToProviderSync: %v", err)
	}
	provider := syncTarget.Provider
	if provider.Name != "qwen-vllm" {
		t.Fatalf("provider.Name = %q", provider.Name)
	}
	if provider.Type != "openai" || provider.Vendor != "vllm" {
		t.Fatalf("provider type/vendor = %s/%s, want openai/vllm", provider.Type, provider.Vendor)
	}
	if provider.BaseURL != "http://qwen-router.llm.svc:8000/v1" {
		t.Fatalf("provider.BaseURL = %q", provider.BaseURL)
	}
	if provider.ModelAliases["qwen-large"] != "Qwen/Qwen3-32B" {
		t.Fatalf("provider.ModelAliases = %#v", provider.ModelAliases)
	}
	if provider.Capabilities.Stream == nil || !*provider.Capabilities.Stream {
		t.Fatalf("provider stream capability not preserved")
	}
	if syncTarget.APIKeySecretRef == nil || syncTarget.APIKeySecretRef.Name != "qwen-secret" {
		t.Fatalf("secret ref not preserved: %#v", syncTarget.APIKeySecretRef)
	}
	if syncTarget.RouteLabels["accelerator"] != "h100" {
		t.Fatalf("route labels not preserved: %#v", syncTarget.RouteLabels)
	}
	if provider.Labels["accelerator"] != "h100" || provider.Labels["runtime"] != "vllm" {
		t.Fatalf("provider labels = %#v, want route labels mirrored", provider.Labels)
	}
}

func TestModelEndpointToProviderSyncRequiresTarget(t *testing.T) {
	_, err := (ModelEndpoint{Metadata: ObjectMeta{Name: "missing"}}).ToProviderSync("default")
	if !errors.Is(err, ErrMissingModelEndpointTarget) {
		t.Fatalf("err = %v, want ErrMissingModelEndpointTarget", err)
	}
}

func TestRoutePolicyToRouterConfig(t *testing.T) {
	hasTools := true
	policy := RoutePolicy{
		Metadata: ObjectMeta{Name: "code-route"},
		Spec: RoutePolicySpec{
			TargetRefs: []TargetRef{
				{Kind: "ModelEndpoint", Name: "coder-vllm"},
				{Kind: "Other", Name: "ignored"},
			},
			ModelSelector: ModelSelector{Names: []string{"gpt-code"}, Aliases: []string{"claude-code"}},
			Match: RouteMatch{
				MinPromptTokens: 1024,
				HasTools:        &hasTools,
				AnyRegex:        []string{"(?i)stack trace"},
			},
			Strategy: "least_gpu_cache",
			EndpointSelector: map[string]string{
				"accelerator": "h100",
			},
		},
	}

	cfg := policy.ToRouterConfig()
	if cfg.Strategy != "least_gpu_cache" {
		t.Fatalf("Strategy = %q", cfg.Strategy)
	}
	if !cfg.RuleEngine.Enabled || len(cfg.RuleEngine.Rules) != 1 {
		t.Fatalf("RuleEngine = %#v", cfg.RuleEngine)
	}
	rule := cfg.RuleEngine.Rules[0]
	if rule.Name != "code-route" {
		t.Fatalf("rule.Name = %q", rule.Name)
	}
	if len(rule.Action.Providers) != 1 || rule.Action.Providers[0] != "coder-vllm" {
		t.Fatalf("providers = %#v", rule.Action.Providers)
	}
	if rule.Action.ProviderLabels["accelerator"] != "h100" {
		t.Fatalf("provider labels = %#v, want endpoint selector", rule.Action.ProviderLabels)
	}
	if len(rule.Match.Models) != 2 || rule.Match.Models[1] != "claude-code" {
		t.Fatalf("models = %#v", rule.Match.Models)
	}
	if rule.Match.HasTools == nil || !*rule.Match.HasTools {
		t.Fatalf("HasTools not preserved")
	}
}

func TestBudgetPolicyToBudgetSync(t *testing.T) {
	policy := BudgetPolicy{
		Spec: BudgetPolicySpec{
			Subject: BudgetSubject{Kind: "tenant", Name: "platform"},
			Limits: BudgetLimits{
				QPS:           3.2,
				MonthlyTokens: 500000,
				MonthlyCost:   250,
			},
			Enforcement:     "soft_alert",
			AlertThresholds: []float64{0.5, 0.8, 0.95},
		},
	}

	target, err := policy.ToBudgetSync()
	if err != nil {
		t.Fatalf("ToBudgetSync: %v", err)
	}
	if target.SubjectKind != "tenant" || target.SubjectName != "platform" {
		t.Fatalf("subject = %s/%s", target.SubjectKind, target.SubjectName)
	}
	if target.BudgetUSD != 250 || target.RateLimitQPS != 4 {
		t.Fatalf("budget sync = %#v", target)
	}
	if target.BudgetPolicy != "soft_alert" {
		t.Fatalf("BudgetPolicy = %q", target.BudgetPolicy)
	}
}

func TestInferenceAutoscalePolicyEvaluate(t *testing.T) {
	policy := InferenceAutoscalePolicy{
		Spec: InferenceAutoscalePolicySpec{
			TargetRef:   TargetRef{Kind: "InferenceService", Name: "qwen"},
			Mode:        "enforce",
			MinReplicas: 1,
			MaxReplicas: 5,
			Metrics: AutoscaleMetricTargets{
				QueueDepth:     10,
				GPUUtilization: 0.85,
			},
			Behavior: AutoscaleBehavior{MaxScaleUpStep: 2},
		},
	}

	decision, err := policy.Evaluate(2, RuntimeSignals{QueueDepth: 12, GPUUtilization: 0.7})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if decision.DesiredReplicas != 4 || !decision.Enforce || decision.Reason != "above_targets" {
		t.Fatalf("scale-up decision = %#v", decision)
	}

	decision, err = policy.Evaluate(2, RuntimeSignals{QueueDepth: 1, GPUUtilization: 0.1})
	if err != nil {
		t.Fatalf("Evaluate low: %v", err)
	}
	if decision.DesiredReplicas != 1 || decision.Reason != "below_targets" {
		t.Fatalf("scale-down decision = %#v", decision)
	}
}
