package main

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/service/platform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestKubernetesStatusWriterPatchesInferenceStatus(t *testing.T) {
	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{
			modelEndpointGVR: "ModelEndpointList",
			routePolicyGVR:   "RoutePolicyList",
			budgetPolicyGVR:  "BudgetPolicyList",
			inferenceSvcGVR:  "InferenceServiceList",
			autoscaleGVR:     "InferenceAutoscalePolicyList",
			deploymentGVR:    "DeploymentList",
		},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "gateyes.io/v1alpha1",
			"kind":       "ModelEndpoint",
			"metadata": map[string]any{
				"name":       "qwen-provider",
				"namespace":  "llm",
				"generation": int64(3),
			},
			"spec": map[string]any{
				"serviceRef": map[string]any{"name": "qwen", "port": int64(8000), "path": "/v1"},
				"model":      "Qwen/Qwen3",
			},
		}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "gateyes.io/v1alpha1",
			"kind":       "RoutePolicy",
			"metadata": map[string]any{
				"name":       "qwen-route",
				"namespace":  "llm",
				"generation": int64(4),
			},
			"spec": map[string]any{
				"targetRefs": []any{map[string]any{"kind": "ModelEndpoint", "name": "qwen-provider"}},
			},
		}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "gateyes.io/v1alpha1",
			"kind":       "BudgetPolicy",
			"metadata": map[string]any{
				"name":       "tenant-budget",
				"namespace":  "llm",
				"generation": int64(5),
			},
			"spec": map[string]any{
				"subject": map[string]any{"kind": "tenant", "name": "tenant-a"},
			},
		}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "gateyes.io/v1alpha1",
			"kind":       "InferenceService",
			"metadata": map[string]any{
				"name":       "qwen",
				"namespace":  "llm",
				"generation": int64(7),
			},
			"spec": map[string]any{"runtime": "vllm", "model": "Qwen/Qwen3"},
		}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "gateyes.io/v1alpha1",
			"kind":       "InferenceAutoscalePolicy",
			"metadata": map[string]any{
				"name":      "scale-qwen",
				"namespace": "llm",
			},
			"spec": map[string]any{
				"targetRef": map[string]any{"kind": "InferenceService", "name": "qwen"},
			},
		}},
		&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      "qwen",
				"namespace": "llm",
			},
			"status": map[string]any{
				"readyReplicas": int64(2),
			},
		}},
	)
	writer := &kubernetesStatusWriter{client: client}
	snapshot := platform.ResourceSnapshot{
		ModelEndpoints: []platform.ModelEndpoint{{
			Metadata: platform.ObjectMeta{Name: "qwen-provider", Namespace: "llm", Generation: 3},
			Spec: platform.ModelEndpointSpec{
				ServiceRef: &platform.ServiceRef{Name: "qwen", Port: 8000, Path: "/v1"},
				Model:      "Qwen/Qwen3",
			},
		}},
		RoutePolicies: []platform.RoutePolicy{{
			Metadata: platform.ObjectMeta{Name: "qwen-route", Namespace: "llm", Generation: 4},
			Spec: platform.RoutePolicySpec{
				TargetRefs: []platform.TargetRef{{Kind: "ModelEndpoint", Name: "qwen-provider"}},
			},
		}},
		BudgetPolicies: []platform.BudgetPolicy{{
			Metadata: platform.ObjectMeta{Name: "tenant-budget", Namespace: "llm", Generation: 5},
			Spec: platform.BudgetPolicySpec{
				Subject: platform.BudgetSubject{Kind: "tenant", Name: "tenant-a"},
			},
		}},
		InferenceServices: []platform.InferenceService{{
			Metadata: platform.ObjectMeta{Name: "qwen", Namespace: "llm", Generation: 7},
			Spec: platform.InferenceServiceSpec{
				Runtime: "vllm",
				Model:   "Qwen/Qwen3",
				Serving: platform.InferenceServingSpec{
					Port:       8000,
					OpenAIPath: "/v1",
				},
			},
		}},
		AutoscalePolicies: []platform.InferenceAutoscalePolicy{{
			Metadata: platform.ObjectMeta{Name: "scale-qwen", Namespace: "llm"},
			Spec: platform.InferenceAutoscalePolicySpec{
				TargetRef: platform.TargetRef{Kind: "InferenceService", Name: "qwen"},
			},
		}},
	}
	plan := platform.SyncPlan{Workloads: platform.InferenceWorkloadPlan{
		AutoscaleDecisions: []platform.AutoscaleDecision{{
			TargetKind:      "InferenceService",
			TargetName:      "qwen",
			Mode:            "enforce",
			CurrentReplicas: 1,
			DesiredReplicas: 2,
			Reason:          "above_targets",
			Enforce:         true,
		}},
	}}

	if err := writer.Update(context.Background(), snapshot, plan, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}

	service, err := client.Resource(inferenceSvcGVR).Namespace("llm").Get(context.Background(), "qwen", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get InferenceService: %v", err)
	}
	endpointRef, _, _ := unstructured.NestedString(service.Object, "status", "endpointRef")
	if endpointRef != "qwen" {
		t.Fatalf("endpointRef = %q, want qwen", endpointRef)
	}
	ready, _, _ := unstructured.NestedInt64(service.Object, "status", "readyReplicas")
	if ready != 2 {
		t.Fatalf("readyReplicas = %d, want 2", ready)
	}

	endpoint, err := client.Resource(modelEndpointGVR).Namespace("llm").Get(context.Background(), "qwen-provider", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get ModelEndpoint: %v", err)
	}
	health, _, _ := unstructured.NestedString(endpoint.Object, "status", "health")
	if health != "unknown" {
		t.Fatalf("health = %q, want unknown", health)
	}
	resolvedURL, _, _ := unstructured.NestedString(endpoint.Object, "status", "resolvedURL")
	if resolvedURL != "http://qwen.llm.svc:8000/v1" {
		t.Fatalf("resolvedURL = %q, want service URL", resolvedURL)
	}

	route, err := client.Resource(routePolicyGVR).Namespace("llm").Get(context.Background(), "qwen-route", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get RoutePolicy: %v", err)
	}
	active, _, _ := unstructured.NestedBool(route.Object, "status", "active")
	if !active {
		t.Fatalf("active = false, want true")
	}

	budget, err := client.Resource(budgetPolicyGVR).Namespace("llm").Get(context.Background(), "tenant-budget", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get BudgetPolicy: %v", err)
	}
	budgetObserved, _, _ := unstructured.NestedInt64(budget.Object, "status", "observedGeneration")
	if budgetObserved != 5 {
		t.Fatalf("budget observedGeneration = %d, want 5", budgetObserved)
	}

	policy, err := client.Resource(autoscaleGVR).Namespace("llm").Get(context.Background(), "scale-qwen", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get InferenceAutoscalePolicy: %v", err)
	}
	desired, _, _ := unstructured.NestedInt64(policy.Object, "status", "desiredReplicas")
	if desired != 2 {
		t.Fatalf("desiredReplicas = %d, want 2", desired)
	}
	reason, _, _ := unstructured.NestedString(policy.Object, "status", "reason")
	if reason != "above_targets" {
		t.Fatalf("reason = %q, want above_targets", reason)
	}
}
