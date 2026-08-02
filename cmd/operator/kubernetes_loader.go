package main

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/gateyes/gateway/internal/service/platform"
)

var (
	modelEndpointGVR = schema.GroupVersionResource{Group: "gateyes.io", Version: "v1alpha1", Resource: "modelendpoints"}
	routePolicyGVR   = schema.GroupVersionResource{Group: "gateyes.io", Version: "v1alpha1", Resource: "routepolicies"}
	budgetPolicyGVR  = schema.GroupVersionResource{Group: "gateyes.io", Version: "v1alpha1", Resource: "budgetpolicies"}
	autoscaleGVR     = schema.GroupVersionResource{Group: "gateyes.io", Version: "v1alpha1", Resource: "inferenceautoscalepolicies"}
)

type kubernetesSnapshotLoader struct {
	client    dynamic.Interface
	namespace string
}

func newKubernetesSnapshotLoader(kubeconfig string, namespace string) (*kubernetesSnapshotLoader, error) {
	restCfg, err := kubernetesRESTConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}
	return &kubernetesSnapshotLoader{client: client, namespace: namespace}, nil
}

func kubernetesRESTConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}
	return clientcmd.BuildConfigFromFlags("", "")
}

func (l *kubernetesSnapshotLoader) Load(ctx context.Context) (platform.ResourceSnapshot, error) {
	var snapshot platform.ResourceSnapshot
	var errs []error

	modelEndpoints, err := l.list(ctx, modelEndpointGVR)
	if err != nil {
		errs = append(errs, fmt.Errorf("list ModelEndpoint: %w", err))
	} else {
		for i := range modelEndpoints {
			item, err := parseModelEndpoint(modelEndpoints[i])
			if err != nil {
				errs = append(errs, fmt.Errorf("parse ModelEndpoint %s: %w", modelEndpoints[i].GetName(), err))
				continue
			}
			snapshot.ModelEndpoints = append(snapshot.ModelEndpoints, item)
		}
	}

	routePolicies, err := l.list(ctx, routePolicyGVR)
	if err != nil {
		errs = append(errs, fmt.Errorf("list RoutePolicy: %w", err))
	} else {
		for i := range routePolicies {
			item, err := parseRoutePolicy(routePolicies[i])
			if err != nil {
				errs = append(errs, fmt.Errorf("parse RoutePolicy %s: %w", routePolicies[i].GetName(), err))
				continue
			}
			snapshot.RoutePolicies = append(snapshot.RoutePolicies, item)
		}
	}

	budgetPolicies, err := l.list(ctx, budgetPolicyGVR)
	if err != nil {
		errs = append(errs, fmt.Errorf("list BudgetPolicy: %w", err))
	} else {
		for i := range budgetPolicies {
			item, err := parseBudgetPolicy(budgetPolicies[i])
			if err != nil {
				errs = append(errs, fmt.Errorf("parse BudgetPolicy %s: %w", budgetPolicies[i].GetName(), err))
				continue
			}
			snapshot.BudgetPolicies = append(snapshot.BudgetPolicies, item)
		}
	}

	autoscalePolicies, err := l.list(ctx, autoscaleGVR)
	if err != nil {
		errs = append(errs, fmt.Errorf("list InferenceAutoscalePolicy: %w", err))
	} else {
		for i := range autoscalePolicies {
			item, err := parseAutoscalePolicy(autoscalePolicies[i])
			if err != nil {
				errs = append(errs, fmt.Errorf("parse InferenceAutoscalePolicy %s: %w", autoscalePolicies[i].GetName(), err))
				continue
			}
			snapshot.AutoscalePolicies = append(snapshot.AutoscalePolicies, item)
		}
	}

	return snapshot, errors.Join(errs...)
}

func (l *kubernetesSnapshotLoader) list(ctx context.Context, gvr schema.GroupVersionResource) ([]unstructured.Unstructured, error) {
	resource := l.client.Resource(gvr)
	if l.namespace != "" {
		list, err := resource.Namespace(l.namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	}
	list, err := resource.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func parseObjectMeta(obj unstructured.Unstructured) platform.ObjectMeta {
	return platform.ObjectMeta{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		Labels:    obj.GetLabels(),
	}
}

func parseModelEndpoint(obj unstructured.Unstructured) (platform.ModelEndpoint, error) {
	content := obj.Object
	enabled, err := nestedBoolPtr(content, "spec", "enabled")
	if err != nil {
		return platform.ModelEndpoint{}, err
	}
	chat, err := nestedBoolPtr(content, "spec", "capabilities", "chat")
	if err != nil {
		return platform.ModelEndpoint{}, err
	}
	responses, err := nestedBoolPtr(content, "spec", "capabilities", "responses")
	if err != nil {
		return platform.ModelEndpoint{}, err
	}
	messages, err := nestedBoolPtr(content, "spec", "capabilities", "messages")
	if err != nil {
		return platform.ModelEndpoint{}, err
	}
	stream, err := nestedBoolPtr(content, "spec", "capabilities", "stream")
	if err != nil {
		return platform.ModelEndpoint{}, err
	}
	tools, err := nestedBoolPtr(content, "spec", "capabilities", "tools")
	if err != nil {
		return platform.ModelEndpoint{}, err
	}
	images, err := nestedBoolPtr(content, "spec", "capabilities", "images")
	if err != nil {
		return platform.ModelEndpoint{}, err
	}
	structured, err := nestedBoolPtr(content, "spec", "capabilities", "structuredOutput")
	if err != nil {
		return platform.ModelEndpoint{}, err
	}
	longContext, err := nestedBoolPtr(content, "spec", "capabilities", "longContext")
	if err != nil {
		return platform.ModelEndpoint{}, err
	}
	embeddings, err := nestedBoolPtr(content, "spec", "capabilities", "embeddings")
	if err != nil {
		return platform.ModelEndpoint{}, err
	}

	return platform.ModelEndpoint{
		Metadata: parseObjectMeta(obj),
		Spec: platform.ModelEndpointSpec{
			Type:            nestedString(content, "spec", "type"),
			Vendor:          nestedString(content, "spec", "vendor"),
			Endpoint:        nestedString(content, "spec", "endpoint"),
			BaseURL:         nestedString(content, "spec", "baseURL"),
			ServiceRef:      parseServiceRef(content, "spec", "serviceRef"),
			APIKeySecretRef: parseSecretKeyRef(content, "spec", "apiKeySecretRef"),
			Model:           nestedString(content, "spec", "model"),
			ModelAliases:    nestedStringMap(content, "spec", "modelAliases"),
			Weight:          nestedInt(content, "spec", "weight"),
			TimeoutSeconds:  nestedInt(content, "spec", "timeoutSeconds"),
			Metrics: platform.MetricsSpec{
				URL:                   nestedString(content, "spec", "metrics", "url"),
				ScrapeIntervalSeconds: nestedInt(content, "spec", "metrics", "scrapeIntervalSeconds"),
			},
			Pricing: platform.PricingSpec{
				Input:  nestedFloat(content, "spec", "pricing", "input"),
				Output: nestedFloat(content, "spec", "pricing", "output"),
			},
			Capabilities: platform.CapabilitySpec{
				Chat:             chat,
				Responses:        responses,
				Messages:         messages,
				Stream:           stream,
				Tools:            tools,
				Images:           images,
				StructuredOutput: structured,
				LongContext:      longContext,
				Embeddings:       embeddings,
			},
			RouteLabels: nestedStringMap(content, "spec", "routeLabels"),
			Drain:       nestedBool(content, "spec", "drain"),
			Enabled:     enabled,
		},
	}, nil
}

func parseRoutePolicy(obj unstructured.Unstructured) (platform.RoutePolicy, error) {
	content := obj.Object
	hasTools, err := nestedBoolPtr(content, "spec", "match", "hasTools")
	if err != nil {
		return platform.RoutePolicy{}, err
	}
	hasImages, err := nestedBoolPtr(content, "spec", "match", "hasImages")
	if err != nil {
		return platform.RoutePolicy{}, err
	}
	hasStructuredOutput, err := nestedBoolPtr(content, "spec", "match", "hasStructuredOutput")
	if err != nil {
		return platform.RoutePolicy{}, err
	}
	stream, err := nestedBoolPtr(content, "spec", "match", "stream")
	if err != nil {
		return platform.RoutePolicy{}, err
	}
	return platform.RoutePolicy{
		Metadata: parseObjectMeta(obj),
		Spec: platform.RoutePolicySpec{
			Priority:         nestedInt(content, "spec", "priority"),
			TargetRefs:       parseTargetRefs(content, "spec", "targetRefs"),
			ModelSelector:    parseModelSelector(content),
			Match:            parseRouteMatch(content, hasTools, hasImages, hasStructuredOutput, stream),
			Strategy:         nestedString(content, "spec", "strategy"),
			EndpointSelector: nestedStringMap(content, "spec", "endpointSelector"),
			Fallback: platform.FallbackSpec{
				Enabled:     nestedBool(content, "spec", "fallback", "enabled"),
				MaxAttempts: nestedInt(content, "spec", "fallback", "maxAttempts"),
				OnStatus:    nestedIntSlice(content, "spec", "fallback", "onStatus"),
			},
		},
	}, nil
}

func parseBudgetPolicy(obj unstructured.Unstructured) (platform.BudgetPolicy, error) {
	content := obj.Object
	return platform.BudgetPolicy{
		Metadata: parseObjectMeta(obj),
		Spec: platform.BudgetPolicySpec{
			Subject: platform.BudgetSubject{
				Kind: nestedString(content, "spec", "subject", "kind"),
				Name: nestedString(content, "spec", "subject", "name"),
			},
			Limits: platform.BudgetLimits{
				QPS:           nestedFloat(content, "spec", "limits", "qps"),
				RPM:           nestedInt(content, "spec", "limits", "rpm"),
				TPM:           nestedInt(content, "spec", "limits", "tpm"),
				RequestBurst:  nestedInt(content, "spec", "limits", "requestBurst"),
				TokenBurst:    nestedInt(content, "spec", "limits", "tokenBurst"),
				MonthlyTokens: int64(nestedInt(content, "spec", "limits", "monthlyTokens")),
				MonthlyCost:   nestedFloat(content, "spec", "limits", "monthlyCost"),
			},
			Enforcement:     nestedString(content, "spec", "enforcement"),
			AlertThresholds: nestedFloatSlice(content, "spec", "alertThresholds"),
			Reset: platform.BudgetReset{
				Period:   nestedString(content, "spec", "reset", "period"),
				Timezone: nestedString(content, "spec", "reset", "timezone"),
			},
		},
	}, nil
}

func parseAutoscalePolicy(obj unstructured.Unstructured) (platform.InferenceAutoscalePolicy, error) {
	content := obj.Object
	return platform.InferenceAutoscalePolicy{
		Metadata: parseObjectMeta(obj),
		Spec: platform.InferenceAutoscalePolicySpec{
			TargetRef: platform.TargetRef{
				Kind: nestedString(content, "spec", "targetRef", "kind"),
				Name: nestedString(content, "spec", "targetRef", "name"),
			},
			Mode:        nestedString(content, "spec", "mode"),
			MinReplicas: nestedInt(content, "spec", "minReplicas"),
			MaxReplicas: nestedInt(content, "spec", "maxReplicas"),
			Metrics: platform.AutoscaleMetricTargets{
				QueueDepth:      nestedInt(content, "spec", "metrics", "queueDepth"),
				RunningRequests: nestedInt(content, "spec", "metrics", "runningRequests"),
				TTFTMs:          nestedInt(content, "spec", "metrics", "ttftMs"),
				P95LatencyMs:    nestedInt(content, "spec", "metrics", "p95LatencyMs"),
				GPUUtilization:  nestedFloat(content, "spec", "metrics", "gpuUtilization"),
				GPUCacheUsage:   nestedFloat(content, "spec", "metrics", "gpuCacheUsage"),
				CPUCacheUsage:   nestedFloat(content, "spec", "metrics", "cpuCacheUsage"),
				TPM:             nestedInt(content, "spec", "metrics", "tpm"),
				RPM:             nestedInt(content, "spec", "metrics", "rpm"),
			},
			Behavior: platform.AutoscaleBehavior{
				ScaleUpStabilizationSeconds:   nestedInt(content, "spec", "behavior", "scaleUpStabilizationSeconds"),
				ScaleDownStabilizationSeconds: nestedInt(content, "spec", "behavior", "scaleDownStabilizationSeconds"),
				MaxScaleUpStep:                nestedInt(content, "spec", "behavior", "maxScaleUpStep"),
				MaxScaleDownStep:              nestedInt(content, "spec", "behavior", "maxScaleDownStep"),
			},
		},
	}, nil
}

func parseServiceRef(obj map[string]any, fields ...string) *platform.ServiceRef {
	nested, found, err := unstructured.NestedMap(obj, fields...)
	if err != nil || !found {
		return nil
	}
	return &platform.ServiceRef{
		Namespace: nestedString(nested, "namespace"),
		Name:      nestedString(nested, "name"),
		Port:      nestedInt(nested, "port"),
		Path:      nestedString(nested, "path"),
	}
}

func parseSecretKeyRef(obj map[string]any, fields ...string) *platform.SecretKeyRef {
	nested, found, err := unstructured.NestedMap(obj, fields...)
	if err != nil || !found {
		return nil
	}
	return &platform.SecretKeyRef{
		Name: nestedString(nested, "name"),
		Key:  nestedString(nested, "key"),
	}
}

func parseTargetRefs(obj map[string]any, fields ...string) []platform.TargetRef {
	items, found, err := unstructured.NestedSlice(obj, fields...)
	if err != nil || !found {
		return nil
	}
	refs := make([]platform.TargetRef, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		refs = append(refs, platform.TargetRef{
			Kind: nestedString(raw, "kind"),
			Name: nestedString(raw, "name"),
		})
	}
	return refs
}

func parseModelSelector(obj map[string]any) platform.ModelSelector {
	return platform.ModelSelector{
		Names:    nestedStringSlice(obj, "spec", "modelSelector", "names"),
		Families: nestedStringSlice(obj, "spec", "modelSelector", "families"),
		Aliases:  nestedStringSlice(obj, "spec", "modelSelector", "aliases"),
	}
}

func parseRouteMatch(obj map[string]any, hasTools *bool, hasImages *bool, hasStructuredOutput *bool, stream *bool) platform.RouteMatch {
	return platform.RouteMatch{
		Headers:             nestedStringMap(obj, "spec", "match", "headers"),
		MinPromptTokens:     nestedInt(obj, "spec", "match", "minPromptTokens"),
		MaxPromptTokens:     nestedInt(obj, "spec", "match", "maxPromptTokens"),
		HasTools:            hasTools,
		HasImages:           hasImages,
		HasStructuredOutput: hasStructuredOutput,
		Stream:              stream,
		AnyRegex:            nestedStringSlice(obj, "spec", "match", "anyRegex"),
	}
}

func nestedString(obj map[string]any, fields ...string) string {
	value, found, err := unstructured.NestedString(obj, fields...)
	if err != nil || !found {
		return ""
	}
	return value
}

func nestedStringMap(obj map[string]any, fields ...string) map[string]string {
	value, found, err := unstructured.NestedStringMap(obj, fields...)
	if err != nil || !found {
		return nil
	}
	return value
}

func nestedStringSlice(obj map[string]any, fields ...string) []string {
	value, found, err := unstructured.NestedStringSlice(obj, fields...)
	if err != nil || !found {
		return nil
	}
	return value
}

func nestedBool(obj map[string]any, fields ...string) bool {
	value, found, err := unstructured.NestedBool(obj, fields...)
	if err != nil || !found {
		return false
	}
	return value
}

func nestedBoolPtr(obj map[string]any, fields ...string) (*bool, error) {
	value, found, err := unstructured.NestedBool(obj, fields...)
	if err != nil || !found {
		return nil, err
	}
	return &value, nil
}

func nestedInt(obj map[string]any, fields ...string) int {
	value, found, err := unstructured.NestedInt64(obj, fields...)
	if err == nil && found {
		return int(value)
	}
	raw, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if err != nil || !found {
		return 0
	}
	switch typed := raw.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func nestedFloat(obj map[string]any, fields ...string) float64 {
	raw, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if err != nil || !found {
		return 0
	}
	switch typed := raw.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		return typed
	default:
		return 0
	}
}

func nestedIntSlice(obj map[string]any, fields ...string) []int {
	items, found, err := unstructured.NestedSlice(obj, fields...)
	if err != nil || !found {
		return nil
	}
	values := make([]int, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case int:
			values = append(values, typed)
		case int64:
			values = append(values, int(typed))
		case float64:
			values = append(values, int(typed))
		}
	}
	return values
}

func nestedFloatSlice(obj map[string]any, fields ...string) []float64 {
	items, found, err := unstructured.NestedSlice(obj, fields...)
	if err != nil || !found {
		return nil
	}
	values := make([]float64, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case int:
			values = append(values, float64(typed))
		case int64:
			values = append(values, float64(typed))
		case float64:
			values = append(values, typed)
		}
	}
	return values
}
