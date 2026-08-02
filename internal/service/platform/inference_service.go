package platform

import (
	"errors"
	"fmt"
	"strings"
)

var ErrMissingInferenceServiceModel = errors.New("inference service requires model")

type InferenceService struct {
	Metadata ObjectMeta
	Spec     InferenceServiceSpec
}

type InferenceServiceSpec struct {
	Runtime               string
	Model                 string
	Image                 string
	Replicas              *int
	Serving               InferenceServingSpec
	Resources             map[string]any
	Roles                 []InferenceRoleSpec
	ModelAdapters         []ModelAdapterSpec
	AutoscalePolicyRef    *TargetRef
	ExposeAsModelEndpoint *bool
	RouteLabels           map[string]string
}

type InferenceServingSpec struct {
	Port            int
	OpenAIPath      string
	MetricsPath     string
	APIKeySecretRef *SecretKeyRef
}

type InferenceRoleSpec struct {
	Name      string
	Type      string
	Replicas  *int
	Resources map[string]any
	Args      []string
}

type ModelAdapterSpec struct {
	Name    string
	Source  string
	Enabled *bool
}

type InferenceWorkloadPlan struct {
	Deployments           []WorkloadDeployment
	Services              []WorkloadService
	AutoscaleDecisions    []AutoscaleDecision
	ExposedModelEndpoints []ModelEndpoint
}

type WorkloadDeployment struct {
	Name      string
	Namespace string
	Labels    map[string]string
	Replicas  int
	Image     string
	Runtime   string
	Model     string
	Port      int
	Args      []string
	Resources map[string]any
}

type WorkloadService struct {
	Name       string
	Namespace  string
	Labels     map[string]string
	Selector   map[string]string
	Port       int
	TargetPort int
}

func BuildInferenceWorkloadPlan(services []InferenceService, policies []InferenceAutoscalePolicy, signals map[string]RuntimeSignals, defaultNamespace string) (InferenceWorkloadPlan, error) {
	var plan InferenceWorkloadPlan
	var errs []error
	policyByName := map[string]InferenceAutoscalePolicy{}
	for _, policy := range policies {
		if policy.Metadata.Name != "" {
			policyByName[policy.Metadata.Name] = policy
		}
	}

	for _, service := range services {
		servicePlan, err := service.ToWorkloadPlan(defaultNamespace, selectAutoscalePolicy(service, policies, policyByName), lookupRuntimeSignals(service, defaultNamespace, signals))
		if err != nil {
			errs = append(errs, fmt.Errorf("inference service %s: %w", service.Metadata.Name, err))
			continue
		}
		plan.Deployments = append(plan.Deployments, servicePlan.Deployments...)
		plan.Services = append(plan.Services, servicePlan.Services...)
		plan.AutoscaleDecisions = append(plan.AutoscaleDecisions, servicePlan.AutoscaleDecisions...)
		plan.ExposedModelEndpoints = append(plan.ExposedModelEndpoints, servicePlan.ExposedModelEndpoints...)
	}
	return plan, errors.Join(errs...)
}

func (s InferenceService) ToWorkloadPlan(defaultNamespace string, policy *InferenceAutoscalePolicy, signals *RuntimeSignals) (InferenceWorkloadPlan, error) {
	if strings.TrimSpace(s.Spec.Model) == "" {
		return InferenceWorkloadPlan{}, ErrMissingInferenceServiceModel
	}
	if strings.EqualFold(s.Spec.Runtime, "external") {
		return InferenceWorkloadPlan{}, nil
	}

	namespace := defaultString(s.Metadata.Namespace, defaultNamespace)
	name := defaultString(s.Metadata.Name, s.Spec.Model)
	port := positiveOrDefault(s.Spec.Serving.Port, 8000)
	replicas := intValueOrDefault(s.Spec.Replicas, 1)
	labels := inferenceServiceLabels(name, s.Spec.Runtime, s.Spec.RouteLabels)
	replicas, decision, hasDecision := enforceReplicas(replicas, policy, signals)

	roles := s.Spec.Roles
	if len(roles) == 0 {
		roles = []InferenceRoleSpec{{Name: "server", Type: "server"}}
	}

	serviceRole := servingRoleName(roles)
	serviceLabels := copyStringMap(labels)
	serviceSelector := copyStringMap(labels)
	if serviceRole != "" {
		serviceSelector["gateyes.io/role"] = serviceRole
	}
	plan := InferenceWorkloadPlan{
		Services: []WorkloadService{{
			Name:       name,
			Namespace:  namespace,
			Labels:     serviceLabels,
			Selector:   serviceSelector,
			Port:       port,
			TargetPort: port,
		}},
	}
	if hasDecision {
		plan.AutoscaleDecisions = append(plan.AutoscaleDecisions, decision)
	}

	for _, role := range roles {
		roleName := defaultString(role.Name, "server")
		roleLabels := copyStringMap(labels)
		roleLabels["gateyes.io/role"] = roleName
		roleReplicas := replicas
		if role.Replicas != nil {
			roleReplicas = intValueOrDefault(role.Replicas, replicas)
		}
		plan.Deployments = append(plan.Deployments, WorkloadDeployment{
			Name:      roleDeploymentName(name, roleName, len(roles)),
			Namespace: namespace,
			Labels:    roleLabels,
			Replicas:  roleReplicas,
			Image:     defaultInferenceImage(s.Spec.Runtime, s.Spec.Image),
			Runtime:   defaultString(s.Spec.Runtime, "vllm"),
			Model:     s.Spec.Model,
			Port:      port,
			Args:      inferenceArgs(s.Spec.Runtime, s.Spec.Model, role.Args),
			Resources: workloadResources(s.Spec.Resources, role.Resources),
		})
	}

	if exposeInferenceService(s.Spec.ExposeAsModelEndpoint) {
		plan.ExposedModelEndpoints = append(plan.ExposedModelEndpoints, s.toModelEndpoint(namespace, port))
	}
	return plan, nil
}

func (s InferenceService) toModelEndpoint(namespace string, port int) ModelEndpoint {
	name := defaultString(s.Metadata.Name, s.Spec.Model)
	servicePath := defaultString(s.Spec.Serving.OpenAIPath, "/v1")
	metricsPath := defaultString(s.Spec.Serving.MetricsPath, "/metrics")
	labels := copyStringMap(s.Spec.RouteLabels)
	if labels == nil {
		labels = map[string]string{}
	}
	if strings.TrimSpace(s.Spec.Runtime) != "" {
		labels["runtime"] = strings.TrimSpace(s.Spec.Runtime)
	}
	return ModelEndpoint{
		Metadata: ObjectMeta{Name: name, Namespace: namespace, Labels: copyStringMap(s.Metadata.Labels)},
		Spec: ModelEndpointSpec{
			Type:            defaultString(s.Spec.Runtime, "vllm"),
			ServiceRef:      &ServiceRef{Namespace: namespace, Name: name, Port: port, Path: servicePath},
			APIKeySecretRef: s.Spec.Serving.APIKeySecretRef,
			Model:           s.Spec.Model,
			Metrics: MetricsSpec{
				URL: serviceBaseURL(ServiceRef{Namespace: namespace, Name: name, Port: port, Path: metricsPath}, namespace),
			},
			RouteLabels: labels,
		},
	}
}

func selectAutoscalePolicy(service InferenceService, policies []InferenceAutoscalePolicy, policyByName map[string]InferenceAutoscalePolicy) *InferenceAutoscalePolicy {
	if service.Spec.AutoscalePolicyRef != nil && strings.TrimSpace(service.Spec.AutoscalePolicyRef.Name) != "" {
		if policy, ok := policyByName[strings.TrimSpace(service.Spec.AutoscalePolicyRef.Name)]; ok {
			return &policy
		}
	}
	for _, policy := range policies {
		if strings.EqualFold(policy.Spec.TargetRef.Kind, "InferenceService") && strings.TrimSpace(policy.Spec.TargetRef.Name) == service.Metadata.Name {
			return &policy
		}
	}
	return nil
}

func lookupRuntimeSignals(service InferenceService, defaultNamespace string, signals map[string]RuntimeSignals) *RuntimeSignals {
	if len(signals) == 0 {
		return nil
	}
	namespace := defaultString(service.Metadata.Namespace, defaultNamespace)
	for _, key := range []string{namespace + "/" + service.Metadata.Name, service.Metadata.Name} {
		if signal, ok := signals[key]; ok {
			return &signal
		}
	}
	return nil
}

func enforceReplicas(replicas int, policy *InferenceAutoscalePolicy, signals *RuntimeSignals) (int, AutoscaleDecision, bool) {
	if policy == nil {
		return replicas, AutoscaleDecision{}, false
	}
	if signals != nil {
		decision, err := policy.Evaluate(replicas, *signals)
		if err != nil {
			return replicas, AutoscaleDecision{}, false
		}
		if decision.Enforce {
			return decision.DesiredReplicas, decision, true
		}
		return replicas, decision, true
	}
	mode := defaultString(policy.Spec.Mode, "recommend")
	desired := replicas
	if mode == "enforce" {
		maxReplicas := policy.Spec.MaxReplicas
		if maxReplicas <= 0 {
			maxReplicas = max(1, replicas)
		}
		desired = clamp(replicas, max(0, policy.Spec.MinReplicas), maxReplicas)
	}
	return desired, AutoscaleDecision{
		Mode:            mode,
		CurrentReplicas: replicas,
		DesiredReplicas: desired,
		Reason:          "within_bounds",
		Enforce:         mode == "enforce" && desired != replicas,
	}, true
}

func inferenceServiceLabels(name string, runtime string, routeLabels map[string]string) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/managed-by":     "gateyes-operator",
		"app.kubernetes.io/name":           name,
		"gateyes.io/inference-service":     name,
		"gateyes.io/inference-workload":    "true",
		"gateyes.io/inference-runtime":     defaultString(runtime, "vllm"),
		"gateyes.io/inference-runtime-api": "openai",
	}
	for key, value := range routeLabels {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			labels[key] = value
		}
	}
	return labels
}

func servingRoleName(roles []InferenceRoleSpec) string {
	if len(roles) == 0 {
		return ""
	}
	for _, role := range roles {
		if strings.EqualFold(role.Type, "router") {
			return defaultString(role.Name, role.Type)
		}
	}
	return defaultString(roles[0].Name, roles[0].Type)
}

func roleDeploymentName(serviceName string, roleName string, roleCount int) string {
	if roleCount <= 1 || roleName == "" || roleName == "server" {
		return serviceName
	}
	return serviceName + "-" + roleName
}

func inferenceArgs(runtime string, model string, extra []string) []string {
	args := []string{}
	switch strings.ToLower(strings.TrimSpace(runtime)) {
	case "", "vllm":
		args = append(args, "--model", model)
	case "sglang":
		args = append(args, "--model-path", model)
	}
	args = append(args, extra...)
	return args
}

func defaultInferenceImage(runtime string, image string) string {
	if strings.TrimSpace(image) != "" {
		return strings.TrimSpace(image)
	}
	switch strings.ToLower(strings.TrimSpace(runtime)) {
	case "sglang":
		return "lmsysorg/sglang:latest"
	case "kserve":
		return "kserve/storage-initializer:latest"
	default:
		return "vllm/vllm-openai:latest"
	}
}

func workloadResources(base map[string]any, role map[string]any) map[string]any {
	if len(role) > 0 {
		return copyAnyMap(role)
	}
	return copyAnyMap(base)
}

func exposeInferenceService(value *bool) bool {
	return value == nil || *value
}

func intValueOrDefault(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func copyAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
