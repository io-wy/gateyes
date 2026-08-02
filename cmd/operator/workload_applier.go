package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/gateyes/gateway/internal/service/platform"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
)

var (
	deploymentGVR = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	serviceGVR    = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}
)

type workloadApplier interface {
	Apply(context.Context, platform.InferenceWorkloadPlan) error
}

type kubernetesWorkloadApplier struct {
	client dynamic.Interface
}

func newKubernetesWorkloadApplier(kubeconfig string) (*kubernetesWorkloadApplier, error) {
	restCfg, err := kubernetesRESTConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}
	return &kubernetesWorkloadApplier{client: client}, nil
}

func (a *kubernetesWorkloadApplier) Apply(ctx context.Context, plan platform.InferenceWorkloadPlan) error {
	var errs []error
	for _, deployment := range plan.Deployments {
		if err := a.apply(ctx, deploymentGVR, deploymentObject(deployment)); err != nil {
			errs = append(errs, fmt.Errorf("apply deployment %s/%s: %w", deployment.Namespace, deployment.Name, err))
		}
	}
	for _, service := range plan.Services {
		if err := a.apply(ctx, serviceGVR, serviceObject(service)); err != nil {
			errs = append(errs, fmt.Errorf("apply service %s/%s: %w", service.Namespace, service.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (a *kubernetesWorkloadApplier) apply(ctx context.Context, gvr schema.GroupVersionResource, desired *unstructured.Unstructured) error {
	resource := a.client.Resource(gvr).Namespace(desired.GetNamespace())
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := resource.Get(ctx, desired.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			_, createErr := resource.Create(ctx, desired, metav1.CreateOptions{})
			return createErr
		}
		if err != nil {
			return err
		}
		desired.SetResourceVersion(current.GetResourceVersion())
		_, err = resource.Update(ctx, desired, metav1.UpdateOptions{})
		return err
	})
}

func deploymentObject(deployment platform.WorkloadDeployment) *unstructured.Unstructured {
	labels := copyStringMap(deployment.Labels)
	selector := workloadSelector(labels)
	container := map[string]any{
		"name":  "server",
		"image": deployment.Image,
		"args":  stringSliceToAny(deployment.Args),
		"ports": []any{
			map[string]any{
				"name":          "http",
				"containerPort": int64(deployment.Port),
			},
		},
	}
	if len(deployment.Resources) > 0 {
		container["resources"] = deployment.Resources
	}
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]any{
				"name":      deployment.Name,
				"namespace": deployment.Namespace,
				"labels":    stringMapToAny(labels),
			},
			"spec": map[string]any{
				"replicas": int64(deployment.Replicas),
				"selector": map[string]any{
					"matchLabels": stringMapToAny(selector),
				},
				"template": map[string]any{
					"metadata": map[string]any{
						"labels": stringMapToAny(labels),
					},
					"spec": map[string]any{
						"containers": []any{container},
					},
				},
			},
		},
	}
}

func serviceObject(service platform.WorkloadService) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]any{
				"name":      service.Name,
				"namespace": service.Namespace,
				"labels":    stringMapToAny(service.Labels),
			},
			"spec": map[string]any{
				"type":     "ClusterIP",
				"selector": stringMapToAny(service.Selector),
				"ports": []any{
					map[string]any{
						"name":       "http",
						"port":       int64(service.Port),
						"targetPort": int64(service.TargetPort),
					},
				},
			},
		},
	}
}

func workloadSelector(labels map[string]string) map[string]string {
	selector := map[string]string{}
	for _, key := range []string{"gateyes.io/inference-service", "gateyes.io/role"} {
		if value := labels[key]; value != "" {
			selector[key] = value
		}
	}
	return selector
}

func stringSliceToAny(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func stringMapToAny(in map[string]string) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
