package main

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestKubernetesSnapshotLoaderParsesGateyesCRDs(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			modelEndpointGVR: "ModelEndpointList",
			routePolicyGVR:   "RoutePolicyList",
			budgetPolicyGVR:  "BudgetPolicyList",
			autoscaleGVR:     "InferenceAutoscalePolicyList",
		},
		newUnstructured("ModelEndpoint", "modelendpoints", "qwen", map[string]any{
			"type": "vllm",
			"serviceRef": map[string]any{
				"name": "qwen-router",
				"port": int64(8000),
				"path": "/v1",
			},
			"model":          "Qwen/Qwen3",
			"weight":         int64(9),
			"routeLabels":    map[string]any{"accelerator": "h100"},
			"modelAliases":   map[string]any{"qwen-large": "Qwen/Qwen3"},
			"timeoutSeconds": int64(30),
			"pricing":        map[string]any{"input": float64(0.1), "output": float64(0.2)},
			"capabilities":   map[string]any{"stream": true},
		}),
		newUnstructured("RoutePolicy", "routepolicies", "code-route", map[string]any{
			"priority": int64(10),
			"strategy": "least_gpu_cache",
			"targetRefs": []any{
				map[string]any{"kind": "ModelEndpoint", "name": "qwen"},
			},
			"modelSelector": map[string]any{"names": []any{"qwen-large"}},
			"match":         map[string]any{"hasTools": true, "anyRegex": []any{"stack trace"}},
		}),
		newUnstructured("BudgetPolicy", "budgetpolicies", "tenant-budget", map[string]any{
			"subject":     map[string]any{"kind": "tenant", "name": "tenant-a"},
			"limits":      map[string]any{"qps": float64(3.2), "monthlyCost": float64(100)},
			"enforcement": "soft_alert",
		}),
		newUnstructured("InferenceAutoscalePolicy", "inferenceautoscalepolicies", "scale-qwen", map[string]any{
			"targetRef":   map[string]any{"kind": "InferenceService", "name": "qwen"},
			"mode":        "enforce",
			"minReplicas": int64(1),
			"maxReplicas": int64(5),
			"metrics":     map[string]any{"queueDepth": int64(8), "gpuUtilization": float64(0.82)},
			"behavior":    map[string]any{"maxScaleUpStep": int64(2)},
		}),
	)

	loader := &kubernetesSnapshotLoader{client: client, namespace: "llm"}
	snapshot, err := loader.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(snapshot.ModelEndpoints) != 1 || snapshot.ModelEndpoints[0].Spec.RouteLabels["accelerator"] != "h100" {
		t.Fatalf("ModelEndpoints = %#v, want parsed route labels", snapshot.ModelEndpoints)
	}
	if snapshot.ModelEndpoints[0].Spec.ServiceRef == nil || snapshot.ModelEndpoints[0].Spec.ServiceRef.Port != 8000 {
		t.Fatalf("ServiceRef = %#v, want parsed service ref", snapshot.ModelEndpoints[0].Spec.ServiceRef)
	}
	if len(snapshot.RoutePolicies) != 1 || snapshot.RoutePolicies[0].Spec.Match.HasTools == nil || !*snapshot.RoutePolicies[0].Spec.Match.HasTools {
		t.Fatalf("RoutePolicies = %#v, want hasTools route policy", snapshot.RoutePolicies)
	}
	if len(snapshot.BudgetPolicies) != 1 || snapshot.BudgetPolicies[0].Spec.Limits.MonthlyCost != 100 {
		t.Fatalf("BudgetPolicies = %#v, want monthly cost", snapshot.BudgetPolicies)
	}
	if len(snapshot.AutoscalePolicies) != 1 || snapshot.AutoscalePolicies[0].Spec.Behavior.MaxScaleUpStep != 2 {
		t.Fatalf("AutoscalePolicies = %#v, want behavior parsed", snapshot.AutoscalePolicies)
	}
}

func newUnstructured(kind string, _ string, name string, spec map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "gateyes.io/v1alpha1",
			"kind":       kind,
			"metadata": map[string]any{
				"name":      name,
				"namespace": "llm",
			},
			"spec": spec,
		},
	}
}
