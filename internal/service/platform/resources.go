package platform

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/gateyes/gateway/internal/app/config"
)

var (
	ErrMissingModelEndpointTarget = errors.New("model endpoint requires baseURL or serviceRef.name")
	ErrMissingBudgetSubject       = errors.New("budget policy requires subject kind and name")
	ErrMissingAutoscaleTarget     = errors.New("autoscale policy requires target kind and name")
)

type ObjectMeta struct {
	Name      string
	Namespace string
	Labels    map[string]string
}

type SecretKeyRef struct {
	Name string
	Key  string
}

type ServiceRef struct {
	Namespace string
	Name      string
	Port      int
	Path      string
}

type ProviderSyncTarget struct {
	Provider        config.ProviderConfig
	APIKeySecretRef *SecretKeyRef
	RouteLabels     map[string]string
}

type ModelEndpoint struct {
	Metadata ObjectMeta
	Spec     ModelEndpointSpec
}

type ModelEndpointSpec struct {
	Type            string
	Vendor          string
	Endpoint        string
	BaseURL         string
	ServiceRef      *ServiceRef
	APIKeySecretRef *SecretKeyRef
	Model           string
	ModelAliases    map[string]string
	Weight          int
	TimeoutSeconds  int
	Metrics         MetricsSpec
	Pricing         PricingSpec
	Capabilities    CapabilitySpec
	RouteLabels     map[string]string
	Drain           bool
	Enabled         *bool
}

type MetricsSpec struct {
	URL                   string
	ScrapeIntervalSeconds int
}

type PricingSpec struct {
	Input  float64
	Output float64
}

type CapabilitySpec struct {
	Chat             *bool
	Responses        *bool
	Messages         *bool
	Stream           *bool
	Tools            *bool
	Images           *bool
	StructuredOutput *bool
	LongContext      *bool
	Embeddings       *bool
}

func (m ModelEndpoint) ToProviderSync(defaultNamespace string) (ProviderSyncTarget, error) {
	name := strings.TrimSpace(m.Metadata.Name)
	if name == "" {
		name = strings.TrimSpace(m.Spec.Model)
	}
	baseURL := strings.TrimSpace(m.Spec.BaseURL)
	if baseURL == "" && m.Spec.ServiceRef != nil {
		baseURL = serviceBaseURL(*m.Spec.ServiceRef, defaultNamespace)
	}
	if baseURL == "" {
		return ProviderSyncTarget{}, ErrMissingModelEndpointTarget
	}

	providerType, vendor := providerTypeAndVendor(m.Spec.Type, m.Spec.Vendor)
	enabled := true
	if m.Spec.Enabled != nil {
		enabled = *m.Spec.Enabled
	}
	cfg := config.ProviderConfig{
		Name:         name,
		Type:         providerType,
		Vendor:       vendor,
		BaseURL:      baseURL,
		Endpoint:     defaultString(m.Spec.Endpoint, "chat"),
		Model:        m.Spec.Model,
		Weight:       positiveOrDefault(m.Spec.Weight, 1),
		PriceInput:   m.Spec.Pricing.Input,
		PriceOutput:  m.Spec.Pricing.Output,
		Timeout:      m.Spec.TimeoutSeconds,
		Enabled:      enabled && !m.Spec.Drain,
		Capabilities: m.Spec.Capabilities.toConfig(),
		MetricsURL:   m.Spec.Metrics.URL,
		ModelAliases: copyStringMap(m.Spec.ModelAliases),
		Labels:       copyStringMap(m.Spec.RouteLabels),
	}
	return ProviderSyncTarget{
		Provider:        cfg,
		APIKeySecretRef: m.Spec.APIKeySecretRef,
		RouteLabels:     copyStringMap(m.Spec.RouteLabels),
	}, nil
}

type RoutePolicy struct {
	Metadata ObjectMeta
	Spec     RoutePolicySpec
}

type RoutePolicySpec struct {
	Priority         int
	TargetRefs       []TargetRef
	ModelSelector    ModelSelector
	Match            RouteMatch
	Strategy         string
	EndpointSelector map[string]string
	Fallback         FallbackSpec
}

type TargetRef struct {
	Kind string
	Name string
}

type ModelSelector struct {
	Names    []string
	Families []string
	Aliases  []string
}

type RouteMatch struct {
	Headers             map[string]string
	MinPromptTokens     int
	MaxPromptTokens     int
	HasTools            *bool
	HasImages           *bool
	HasStructuredOutput *bool
	Stream              *bool
	AnyRegex            []string
}

type FallbackSpec struct {
	Enabled     bool
	MaxAttempts int
	OnStatus    []int
}

func (p RoutePolicy) ToRouterConfig() config.RouterConfig {
	strategy := defaultString(p.Spec.Strategy, "least_load")
	providers := targetProviderNames(p.Spec.TargetRefs)
	models := append([]string{}, p.Spec.ModelSelector.Names...)
	models = append(models, p.Spec.ModelSelector.Aliases...)

	ruleEnabled := len(providers) > 0 || len(models) > 0 || p.Spec.Match.hasConditions()
	rule := config.RouteRuleConfig{
		Name: defaultString(p.Metadata.Name, "route-policy"),
		Match: config.RouteMatchConfig{
			Models:              models,
			MinPromptTokens:     p.Spec.Match.MinPromptTokens,
			MaxPromptTokens:     p.Spec.Match.MaxPromptTokens,
			HasTools:            p.Spec.Match.HasTools,
			HasImages:           p.Spec.Match.HasImages,
			HasStructuredOutput: p.Spec.Match.HasStructuredOutput,
			Stream:              p.Spec.Match.Stream,
			AnyRegex:            append([]string{}, p.Spec.Match.AnyRegex...),
		},
		Action: config.RouteActionConfig{
			Providers:      providers,
			ProviderLabels: copyStringMap(p.Spec.EndpointSelector),
		},
	}
	routerCfg := config.RouterConfig{Strategy: strategy}
	if ruleEnabled {
		routerCfg.RuleEngine.Enabled = true
		routerCfg.RuleEngine.Rules = []config.RouteRuleConfig{rule}
	}
	return routerCfg
}

type BudgetPolicy struct {
	Metadata ObjectMeta
	Spec     BudgetPolicySpec
}

type BudgetPolicySpec struct {
	Subject         BudgetSubject
	Limits          BudgetLimits
	Enforcement     string
	AlertThresholds []float64
	Reset           BudgetReset
}

type BudgetSubject struct {
	Kind string
	Name string
}

type BudgetLimits struct {
	QPS           float64
	RPM           int
	TPM           int
	RequestBurst  int
	TokenBurst    int
	MonthlyTokens int64
	MonthlyCost   float64
}

type BudgetReset struct {
	Period   string
	Timezone string
}

type BudgetSyncTarget struct {
	SubjectKind     string
	SubjectName     string
	BudgetUSD       float64
	BudgetPolicy    string
	RateLimitQPS    int
	MonthlyTokens   int64
	AlertThresholds []float64
}

func (p BudgetPolicy) ToBudgetSync() (BudgetSyncTarget, error) {
	if strings.TrimSpace(p.Spec.Subject.Kind) == "" || strings.TrimSpace(p.Spec.Subject.Name) == "" {
		return BudgetSyncTarget{}, ErrMissingBudgetSubject
	}
	return BudgetSyncTarget{
		SubjectKind:     strings.TrimSpace(p.Spec.Subject.Kind),
		SubjectName:     strings.TrimSpace(p.Spec.Subject.Name),
		BudgetUSD:       p.Spec.Limits.MonthlyCost,
		BudgetPolicy:    defaultString(p.Spec.Enforcement, "hard_reject"),
		RateLimitQPS:    int(math.Ceil(p.Spec.Limits.QPS)),
		MonthlyTokens:   p.Spec.Limits.MonthlyTokens,
		AlertThresholds: append([]float64{}, p.Spec.AlertThresholds...),
	}, nil
}

type InferenceAutoscalePolicy struct {
	Metadata ObjectMeta
	Spec     InferenceAutoscalePolicySpec
}

type InferenceAutoscalePolicySpec struct {
	TargetRef   TargetRef
	Mode        string
	MinReplicas int
	MaxReplicas int
	Metrics     AutoscaleMetricTargets
	Behavior    AutoscaleBehavior
}

type AutoscaleMetricTargets struct {
	QueueDepth      int
	RunningRequests int
	TTFTMs          int
	P95LatencyMs    int
	GPUUtilization  float64
	GPUCacheUsage   float64
	CPUCacheUsage   float64
	TPM             int
	RPM             int
}

type AutoscaleBehavior struct {
	ScaleUpStabilizationSeconds   int
	ScaleDownStabilizationSeconds int
	MaxScaleUpStep                int
	MaxScaleDownStep              int
}

type RuntimeSignals struct {
	QueueDepth      float64
	RunningRequests float64
	TTFTMs          float64
	P95LatencyMs    float64
	GPUUtilization  float64
	GPUCacheUsage   float64
	CPUCacheUsage   float64
	TPM             float64
	RPM             float64
}

type AutoscaleDecision struct {
	Mode            string
	CurrentReplicas int
	DesiredReplicas int
	Reason          string
	Enforce         bool
}

func (p InferenceAutoscalePolicy) Evaluate(currentReplicas int, signals RuntimeSignals) (AutoscaleDecision, error) {
	if strings.TrimSpace(p.Spec.TargetRef.Kind) == "" || strings.TrimSpace(p.Spec.TargetRef.Name) == "" {
		return AutoscaleDecision{}, ErrMissingAutoscaleTarget
	}
	minReplicas := p.Spec.MinReplicas
	if minReplicas < 0 {
		minReplicas = 0
	}
	maxReplicas := p.Spec.MaxReplicas
	if maxReplicas <= 0 {
		maxReplicas = max(1, currentReplicas)
	}
	desired := clamp(currentReplicas, minReplicas, maxReplicas)
	reason := "within_targets"

	if exceedsAutoscaleTargets(p.Spec.Metrics, signals) {
		step := positiveOrDefault(p.Spec.Behavior.MaxScaleUpStep, 1)
		desired = clamp(currentReplicas+step, minReplicas, maxReplicas)
		reason = "above_targets"
	} else if belowAutoscaleTargets(p.Spec.Metrics, signals) {
		step := positiveOrDefault(p.Spec.Behavior.MaxScaleDownStep, 1)
		desired = clamp(currentReplicas-step, minReplicas, maxReplicas)
		reason = "below_targets"
	}

	mode := defaultString(p.Spec.Mode, "recommend")
	return AutoscaleDecision{
		Mode:            mode,
		CurrentReplicas: currentReplicas,
		DesiredReplicas: desired,
		Reason:          reason,
		Enforce:         mode == "enforce" && desired != currentReplicas,
	}, nil
}

func serviceBaseURL(ref ServiceRef, defaultNamespace string) string {
	if strings.TrimSpace(ref.Name) == "" {
		return ""
	}
	namespace := defaultString(ref.Namespace, defaultNamespace)
	if namespace == "" {
		namespace = "default"
	}
	port := ref.Port
	if port <= 0 {
		port = 80
	}
	path := defaultString(ref.Path, "/v1")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("http://%s.%s.svc:%d%s", ref.Name, namespace, port, path)
}

func providerTypeAndVendor(rawType string, rawVendor string) (string, string) {
	providerType := strings.ToLower(strings.TrimSpace(rawType))
	vendor := strings.ToLower(strings.TrimSpace(rawVendor))
	switch providerType {
	case "vllm", "sglang", "kserve", "external":
		if vendor == "" {
			vendor = providerType
		}
		return "openai", vendor
	case "":
		return "openai", vendor
	default:
		if vendor == "" {
			vendor = providerType
		}
		return providerType, vendor
	}
}

func (c CapabilitySpec) toConfig() config.ProviderCapabilitiesConfig {
	return config.ProviderCapabilitiesConfig{
		Chat:             c.Chat,
		Responses:        c.Responses,
		Messages:         c.Messages,
		Stream:           c.Stream,
		Tools:            c.Tools,
		Images:           c.Images,
		StructuredOutput: c.StructuredOutput,
		LongContext:      c.LongContext,
		Embeddings:       c.Embeddings,
	}
}

func (m RouteMatch) hasConditions() bool {
	return len(m.Headers) > 0 ||
		m.MinPromptTokens > 0 ||
		m.MaxPromptTokens > 0 ||
		m.HasTools != nil ||
		m.HasImages != nil ||
		m.HasStructuredOutput != nil ||
		m.Stream != nil ||
		len(m.AnyRegex) > 0
}

func targetProviderNames(refs []TargetRef) []string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref.Kind != "" && !strings.EqualFold(ref.Kind, "ModelEndpoint") {
			continue
		}
		if strings.TrimSpace(ref.Name) != "" {
			names = append(names, strings.TrimSpace(ref.Name))
		}
	}
	return names
}

func exceedsAutoscaleTargets(targets AutoscaleMetricTargets, signals RuntimeSignals) bool {
	return aboveFloat(signals.QueueDepth, targets.QueueDepth) ||
		aboveFloat(signals.RunningRequests, targets.RunningRequests) ||
		aboveFloat(signals.TTFTMs, targets.TTFTMs) ||
		aboveFloat(signals.P95LatencyMs, targets.P95LatencyMs) ||
		aboveRatio(signals.GPUUtilization, targets.GPUUtilization) ||
		aboveRatio(signals.GPUCacheUsage, targets.GPUCacheUsage) ||
		aboveRatio(signals.CPUCacheUsage, targets.CPUCacheUsage) ||
		aboveFloat(signals.TPM, targets.TPM) ||
		aboveFloat(signals.RPM, targets.RPM)
}

func belowAutoscaleTargets(targets AutoscaleMetricTargets, signals RuntimeSignals) bool {
	return belowFloat(signals.QueueDepth, targets.QueueDepth) &&
		belowFloat(signals.RunningRequests, targets.RunningRequests) &&
		belowFloat(signals.TTFTMs, targets.TTFTMs) &&
		belowFloat(signals.P95LatencyMs, targets.P95LatencyMs) &&
		belowRatio(signals.GPUUtilization, targets.GPUUtilization) &&
		belowRatio(signals.GPUCacheUsage, targets.GPUCacheUsage) &&
		belowRatio(signals.CPUCacheUsage, targets.CPUCacheUsage) &&
		belowFloat(signals.TPM, targets.TPM) &&
		belowFloat(signals.RPM, targets.RPM)
}

func aboveFloat(observed float64, target int) bool {
	return target > 0 && observed > float64(target)
}

func belowFloat(observed float64, target int) bool {
	return target <= 0 || observed < float64(target)*0.5
}

func aboveRatio(observed float64, target float64) bool {
	return target > 0 && observed > target
}

func belowRatio(observed float64, target float64) bool {
	return target <= 0 || observed < target*0.5
}

func clamp(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func positiveOrDefault(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
