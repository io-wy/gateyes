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
			inferenceSvcGVR: "InferenceServiceList",
			autoscaleGVR:    "InferenceAutoscalePolicyList",
			deploymentGVR:   "DeploymentList",
		},
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
