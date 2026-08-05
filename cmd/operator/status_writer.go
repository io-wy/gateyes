package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gateyes/gateway/internal/service/platform"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

type statusWriter interface {
	Update(context.Context, platform.ResourceSnapshot, platform.SyncPlan, error) error
}

type kubernetesStatusWriter struct {
	client dynamic.Interface
	now    func() time.Time
}

func newKubernetesStatusWriter(kubeconfig string) (*kubernetesStatusWriter, error) {
	restCfg, err := kubernetesRESTConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	client, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}
	return &kubernetesStatusWriter{client: client}, nil
}

func (w *kubernetesStatusWriter) Update(ctx context.Context, snapshot platform.ResourceSnapshot, plan platform.SyncPlan, reconcileErr error) error {
	var errs []error
	for _, service := range snapshot.InferenceServices {
		if err := w.patchInferenceServiceStatus(ctx, service, reconcileErr); err != nil {
			errs = append(errs, fmt.Errorf("patch InferenceService %s/%s status: %w", service.Metadata.Namespace, service.Metadata.Name, err))
		}
	}
	decisions := autoscaleDecisionsByTarget(plan.Workloads.AutoscaleDecisions)
	for _, policy := range snapshot.AutoscalePolicies {
		decision, ok := decisions[autoscaleDecisionKey(policy.Metadata.Namespace, policy.Spec.TargetRef.Kind, policy.Spec.TargetRef.Name)]
		if !ok {
			decision, ok = decisions[autoscaleDecisionFallbackKey(policy.Spec.TargetRef.Kind, policy.Spec.TargetRef.Name)]
			if !ok {
				continue
			}
		}
		if err := w.patchAutoscalePolicyStatus(ctx, policy, decision, reconcileErr); err != nil {
			errs = append(errs, fmt.Errorf("patch InferenceAutoscalePolicy %s/%s status: %w", policy.Metadata.Namespace, policy.Metadata.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (w *kubernetesStatusWriter) patchInferenceServiceStatus(ctx context.Context, service platform.InferenceService, reconcileErr error) error {
	namespace := defaultNamespace(service.Metadata.Namespace)
	port := service.Spec.Serving.Port
	if port <= 0 {
		port = 8000
	}
	readyReplicas := w.readyReplicas(ctx, service.Metadata.Namespace, service.Metadata.Name)
	status := map[string]any{
		"endpointRef":        service.Metadata.Name,
		"readyReplicas":      readyReplicas,
		"observedGeneration": service.Metadata.Generation,
		"conditions":         []any{condition("Ready", reconcileErr == nil, conditionReason(reconcileErr), conditionMessage(reconcileErr), w.timestamp())},
	}
	if service.Metadata.Namespace == "" {
		status["endpointRef"] = fmt.Sprintf("%s.%s.svc:%d", service.Metadata.Name, namespace, port)
	}
	return w.patchStatus(ctx, inferenceSvcGVR, namespace, service.Metadata.Name, status)
}

func (w *kubernetesStatusWriter) patchAutoscalePolicyStatus(ctx context.Context, policy platform.InferenceAutoscalePolicy, decision platform.AutoscaleDecision, reconcileErr error) error {
	namespace := defaultNamespace(policy.Metadata.Namespace)
	status := map[string]any{
		"currentReplicas":    int64(decision.CurrentReplicas),
		"desiredReplicas":    int64(decision.DesiredReplicas),
		"mode":               decision.Mode,
		"reason":             decision.Reason,
		"observedGeneration": policy.Metadata.Generation,
		"conditions":         []any{condition("Reconciled", reconcileErr == nil, conditionReason(reconcileErr), conditionMessage(reconcileErr), w.timestamp())},
	}
	return w.patchStatus(ctx, autoscaleGVR, namespace, policy.Metadata.Name, status)
}

func (w *kubernetesStatusWriter) readyReplicas(ctx context.Context, namespace string, name string) int64 {
	deployment, err := w.client.Resource(deploymentGVR).Namespace(defaultNamespace(namespace)).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return 0
	}
	ready, _, _ := unstructured.NestedInt64(deployment.Object, "status", "readyReplicas")
	return ready
}

func (w *kubernetesStatusWriter) patchStatus(ctx context.Context, gvr schema.GroupVersionResource, namespace string, name string, status map[string]any) error {
	body, err := json.Marshal(map[string]any{"status": status})
	if err != nil {
		return err
	}
	_, err = w.client.Resource(gvr).Namespace(defaultNamespace(namespace)).Patch(ctx, name, types.MergePatchType, body, metav1.PatchOptions{}, "status")
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func autoscaleDecisionsByTarget(decisions []platform.AutoscaleDecision) map[string]platform.AutoscaleDecision {
	out := make(map[string]platform.AutoscaleDecision, len(decisions))
	for _, decision := range decisions {
		if decision.TargetName != "" {
			if strings.TrimSpace(decision.TargetNamespace) != "" {
				out[autoscaleDecisionKey(decision.TargetNamespace, decision.TargetKind, decision.TargetName)] = decision
			}
			out[autoscaleDecisionFallbackKey(decision.TargetKind, decision.TargetName)] = decision
		}
	}
	return out
}

func autoscaleDecisionKey(namespace string, kind string, name string) string {
	return defaultNamespace(namespace) + "/" + strings.ToLower(strings.TrimSpace(kind)) + "/" + strings.TrimSpace(name)
}

func autoscaleDecisionFallbackKey(kind string, name string) string {
	return strings.ToLower(strings.TrimSpace(kind)) + "/" + strings.TrimSpace(name)
}

func condition(conditionType string, ok bool, reason string, message string, ts string) map[string]any {
	status := "False"
	if ok {
		status = "True"
	}
	return map[string]any{
		"type":               conditionType,
		"status":             status,
		"reason":             reason,
		"message":            message,
		"lastTransitionTime": ts,
	}
}

func conditionReason(err error) string {
	if err != nil {
		return "ReconcileFailed"
	}
	return "ReconcileSucceeded"
}

func conditionMessage(err error) string {
	if err != nil {
		return err.Error()
	}
	return "reconcile completed"
}

func (w *kubernetesStatusWriter) timestamp() string {
	if w.now != nil {
		return w.now().UTC().Format(time.RFC3339)
	}
	return time.Now().UTC().Format(time.RFC3339)
}
