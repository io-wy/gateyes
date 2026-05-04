package ingress

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/discovery"
	"github.com/gateyes/gateway/internal/proxy"
)

// Controller reconciles Ingress resources into the RouteTable.
type Controller struct {
	client.Client
	Scheme      *runtime.Scheme
	RouteTable  *RouteTable
	Discovery   *discovery.Registry
	TLSManager  *TLSManager
	Config      config.IngressConfig
}

// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/status,verbs=get
// +kubebuilder:rbac:groups="",resources=services;endpoints;secrets,verbs=get;list;watch

func (r *Controller) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var ing networkingv1.Ingress
	if err := r.Get(ctx, req.NamespacedName, &ing); err != nil {
		if errors.IsNotFound(err) {
			// Ingress deleted; rebuild full table.
			return ctrl.Result{}, r.rebuildAll(ctx)
		}
		return ctrl.Result{}, err
	}

	// Filter by ingress class.
	if !r.matchesClass(ing) {
		return ctrl.Result{}, nil
	}

	logger.Info("reconciling ingress", "name", ing.Name, "namespace", ing.Namespace)

	// Build routes from this Ingress.
	routes := r.ingressToRoutes(ctx, ing)

	// Merge with routes from other Ingresses.
	allRoutes, err := r.collectAllRoutes(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Replace routes from this Ingress, keep others.
	var merged []proxy.RouteRule
	for _, rr := range allRoutes {
		if rr.ID == ingressRouteID(ing.Namespace, ing.Name) {
			continue
		}
		merged = append(merged, rr)
	}
	merged = append(merged, routes...)

	r.RouteTable.Replace(merged)
	logger.Info("route table updated", "routes", len(merged))

	// Sync TLS certificates for this ingress.
	if r.TLSManager != nil {
		if err := r.syncTLS(ctx, ing); err != nil {
			logger.Error(err, "failed to sync TLS")
		}
	}

	return ctrl.Result{}, nil
}

func (r *Controller) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Complete(r)
}

func (r *Controller) matchesClass(ing networkingv1.Ingress) bool {
	if r.Config.Class == "" {
		return true
	}
	// Check spec.ingressClassName.
	if ing.Spec.IngressClassName != nil && *ing.Spec.IngressClassName == r.Config.Class {
		return true
	}
	// Fallback to annotation.
	if ing.Annotations["kubernetes.io/ingress.class"] == r.Config.Class {
		return true
	}
	return false
}

func (r *Controller) ingressToRoutes(ctx context.Context, ing networkingv1.Ingress) []proxy.RouteRule {
	annot := ParseAnnotations(ing.Annotations)
	var routes []proxy.RouteRule
	baseID := ingressRouteID(ing.Namespace, ing.Name)

	for _, rule := range ing.Spec.Rules {
		host := rule.Host
		for _, path := range rule.HTTP.Paths {
			pathType := proxy.PathTypePrefix
			switch *path.PathType {
			case networkingv1.PathTypeExact:
				pathType = proxy.PathTypeExact
			case networkingv1.PathTypeImplementationSpecific:
				pathType = proxy.PathTypeRegular
			}

			backendName := fmt.Sprintf("%s/%s", ing.Namespace, path.Backend.Service.Name)
			endpoints, err := r.resolveBackend(ctx, backendName, path.Backend.Service.Port)
			if err != nil {
				slog.Warn("failed to resolve backend", "ingress", ing.Name, "backend", backendName, "error", err)
				continue
			}

			pool := proxy.NewBackendPool(endpoints)
			routes = append(routes, proxy.RouteRule{
				ID:          fmt.Sprintf("%s-%s-%s", baseID, host, path.Path),
				Host:        host,
				Path:        path.Path,
				PathType:    pathType,
				BackendPool: pool,
				Annotations: annot,
			})
		}
	}

	// TLS is handled separately by tls_manager; here we just note which hosts have TLS.
	return routes
}

func (r *Controller) resolveBackend(ctx context.Context, serviceName string, port networkingv1.ServiceBackendPort) ([]proxy.Backend, error) {
	endpoints, err := r.Discovery.Discover(ctx, r.Discovery.DefaultType(), serviceName)
	if err != nil {
		return nil, err
	}

	var backends []proxy.Backend
	proto := "http"
	for _, ep := range endpoints {
		addr := ep.Address
		if port.Number != 0 {
			addr = fmt.Sprintf("%s:%d", stripPort(addr), port.Number)
		} else if port.Name != "" {
			// For static/K8s, port name may not resolve; keep original address port.
		}
		backends = append(backends, proxy.NewBackend(
			fmt.Sprintf("%s-%s", serviceName, addr),
			addr,
			proto,
			ep.Weight,
		))
	}
	return backends, nil
}

func (r *Controller) collectAllRoutes(ctx context.Context) ([]proxy.RouteRule, error) {
	return r.RouteTable.List(), nil
}

func (r *Controller) rebuildAll(ctx context.Context) error {
	var ingressList networkingv1.IngressList
	if err := r.List(ctx, &ingressList); err != nil {
		return err
	}
	var all []proxy.RouteRule
	for _, ing := range ingressList.Items {
		if !r.matchesClass(ing) {
			continue
		}
		all = append(all, r.ingressToRoutes(ctx, ing)...)
	}
	r.RouteTable.Replace(all)

	if r.TLSManager != nil {
		for _, ing := range ingressList.Items {
			if !r.matchesClass(ing) {
				continue
			}
			if err := r.syncTLS(ctx, ing); err != nil {
				slog.Warn("failed to sync TLS during rebuild", "ingress", ing.Name, "error", err)
			}
		}
	}
	return nil
}

func (r *Controller) syncTLS(ctx context.Context, ing networkingv1.Ingress) error {
	for _, tls := range ing.Spec.TLS {
		for _, host := range tls.Hosts {
			if err := r.TLSManager.Load(ctx, host, tls.SecretName, ing.Namespace); err != nil {
				return fmt.Errorf("load TLS for host %s from secret %s/%s: %w", host, ing.Namespace, tls.SecretName, err)
			}
		}
	}
	return nil
}

func ingressRouteID(ns, name string) string {
	return fmt.Sprintf("%s/%s", ns, name)
}

func stripPort(hostport string) string {
	if i := strings.LastIndex(hostport, ":"); i != -1 {
		return hostport[:i]
	}
	return hostport
}
