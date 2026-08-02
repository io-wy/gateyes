package main

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/service/platform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestKubernetesWorkloadApplierCreatesDeploymentAndService(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	applier := &kubernetesWorkloadApplier{client: client}
	plan := platform.InferenceWorkloadPlan{
		Deployments: []platform.WorkloadDeployment{{
			Name:      "qwen",
			Namespace: "llm",
			Labels: map[string]string{
				"gateyes.io/inference-service": "qwen",
				"gateyes.io/role":              "server",
			},
			Replicas: 3,
			Image:    "registry.local/qwen:v1",
			Runtime:  "vllm",
			Model:    "Qwen/Qwen3",
			Port:     8000,
			Args:     []string{"--model", "Qwen/Qwen3"},
		}},
		Services: []platform.WorkloadService{{
			Name:      "qwen",
			Namespace: "llm",
			Labels: map[string]string{
				"gateyes.io/inference-service": "qwen",
			},
			Selector: map[string]string{
				"gateyes.io/inference-service": "qwen",
				"gateyes.io/role":              "server",
			},
			Port:       8000,
			TargetPort: 8000,
		}},
	}

	if err := applier.Apply(context.Background(), plan); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	deployment, err := client.Resource(deploymentGVR).Namespace("llm").Get(context.Background(), "qwen", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	replicas, _, _ := unstructured.NestedInt64(deployment.Object, "spec", "replicas")
	if replicas != 3 {
		t.Fatalf("deployment replicas = %d, want 3", replicas)
	}
	containers, _, _ := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	if len(containers) != 1 {
		t.Fatalf("deployment containers = %#v, want one container", containers)
	}
	container, ok := containers[0].(map[string]any)
	if !ok {
		t.Fatalf("deployment container = %#v, want object", containers[0])
	}
	image, _, _ := unstructured.NestedString(container, "image")
	if image != "registry.local/qwen:v1" {
		t.Fatalf("deployment image = %q", image)
	}
	args, _, _ := unstructured.NestedStringSlice(container, "args")
	if len(args) != 2 || args[0] != "--model" || args[1] != "Qwen/Qwen3" {
		t.Fatalf("deployment args = %#v", args)
	}

	service, err := client.Resource(serviceGVR).Namespace("llm").Get(context.Background(), "qwen", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	selector, _, _ := unstructured.NestedStringMap(service.Object, "spec", "selector")
	if selector["gateyes.io/role"] != "server" {
		t.Fatalf("service selector = %#v, want role selector", selector)
	}
}
