package controllers

import (
	"context"
	"strings"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"

	api "github.com/gateyes/gateway/api/v1alpha1"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ProviderReconciler reconciles a Provider object.
type ProviderReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	ProviderMgr *provider.Manager
	Store       repository.ProviderRegistryStore
}

// +kubebuilder:rbac:groups=gateway.gateyes.io,resources=providers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=gateway.gateyes.io,resources=providers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.gateyes.io,resources=providers/finalizers,verbs=update

// Reconcile is called whenever a Provider CR changes.
func (r *ProviderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var p api.Provider
	if err := r.Get(ctx, req.NamespacedName, &p); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("reconciling provider", "name", p.Name, "enabled", p.Spec.Enabled)

	// Convert CR spec to internal config and registry record.
	cfg := providerConfigFromCR(p)
	record := provider.DefaultRegistryRecordFromConfig(cfg)

	// Upsert into runtime provider manager.
	if err := r.ProviderMgr.UpsertRuntimeProvider(record); err != nil {
		log.Error(err, "failed to upsert runtime provider", "provider", p.Name)
		if updateErr := r.setCondition(ctx, &p, metav1.ConditionFalse, "RuntimeUpsertFailed", err.Error()); updateErr != nil {
			log.Error(updateErr, "failed to update provider status")
		}
		return ctrl.Result{}, err
	}

	// Persist to database so non-K8s code paths see the update.
	if r.Store != nil {
		if err := r.Store.UpsertProviderRegistry(ctx, record); err != nil {
			log.Error(err, "failed to upsert provider registry in DB", "provider", p.Name)
			if updateErr := r.setCondition(ctx, &p, metav1.ConditionFalse, "DBUpsertFailed", err.Error()); updateErr != nil {
				log.Error(updateErr, "failed to update provider status")
			}
			return ctrl.Result{}, err
		}
	}

	// Update status to reflect successful reconciliation.
	if err := r.setCondition(ctx, &p, metav1.ConditionTrue, "Reconciled", "Provider runtime updated successfully"); err != nil {
		log.Error(err, "failed to update provider status")
		return ctrl.Result{}, err
	}

	log.Info("provider reconciled", "name", p.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProviderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&api.Provider{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}

func (r *ProviderReconciler) setCondition(ctx context.Context, p *api.Provider, status metav1.ConditionStatus, reason, message string) error {
	now := metav1.Now()
	p.Status.Ready = status == metav1.ConditionTrue
	p.Status.LastSyncTime = &now
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             status,
		ObservedGeneration: p.Generation,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
	})
	return r.Status().Update(ctx, p)
}

// providerConfigFromCR converts a Provider CR spec to the internal config.ProviderConfig.
func providerConfigFromCR(p api.Provider) config.ProviderConfig {
	return config.ProviderConfig{
		Name:          p.Name,
		Type:          p.Spec.Type,
		Vendor:        p.Spec.Vendor,
		BaseURL:       p.Spec.BaseURL,
		GRPCTarget:    p.Spec.GRPCTarget,
		GRPCUseTLS:    p.Spec.GRPCUseTLS,
		GRPCAuthority: p.Spec.GRPCAuthority,
		APIKey:        p.Spec.APIKey,
		Model:         p.Spec.Model,
		Weight:        p.Spec.Weight,
		PriceInput:    p.Spec.PriceInput,
		PriceOutput:   p.Spec.PriceOutput,
		MaxTokens:     p.Spec.MaxTokens,
		Timeout:       p.Spec.Timeout,
		Enabled:       p.Spec.Enabled,
		Headers:       p.Spec.Headers,
		ExtraBody:     p.Spec.ExtraBody,
		Endpoint:      strings.ToLower(strings.TrimSpace(p.Spec.Endpoint)),
	}
}
