package discovery

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// K8sEndpointsDiscovery discovers endpoints via Kubernetes Endpoints / EndpointSlice.
type K8sEndpointsDiscovery struct {
	client    client.Reader
	namespace string
	cache     map[string][]Endpoint
	mu        sync.RWMutex
}

func NewK8sEndpointsDiscovery(c client.Reader, namespace string) *K8sEndpointsDiscovery {
	return &K8sEndpointsDiscovery{
		client:    c,
		namespace: namespace,
		cache:     make(map[string][]Endpoint),
	}
}

func (d *K8sEndpointsDiscovery) Watch(ctx context.Context, serviceName string) ([]Endpoint, error) {
	ns := d.namespace
	if ns == "" {
		// Parse namespace from serviceName if qualified: namespace/name
		parts := strings.SplitN(serviceName, "/", 2)
		if len(parts) == 2 {
			ns = parts[0]
			serviceName = parts[1]
		} else {
			ns = "default"
		}
	}

	// Try EndpointSlice first (modern API).
	var slices discoveryv1.EndpointSliceList
	selector := client.MatchingLabels{"kubernetes.io/service-name": serviceName}
	if ns != "" {
		selector = client.InNamespace(ns)
	}
	if err := d.client.List(ctx, &slices, selector); err == nil && len(slices.Items) > 0 {
		return d.fromEndpointSlices(slices.Items), nil
	}

	// Fallback to legacy Endpoints.
	var endpoints corev1.Endpoints
	key := types.NamespacedName{Name: serviceName, Namespace: ns}
	if err := d.client.Get(ctx, key, &endpoints); err != nil {
		return nil, fmt.Errorf("get endpoints for %s/%s: %w", ns, serviceName, err)
	}
	return d.fromEndpoints(endpoints), nil
}

func (d *K8sEndpointsDiscovery) fromEndpointSlices(slices []discoveryv1.EndpointSlice) []Endpoint {
	var out []Endpoint
	for _, sl := range slices {
		port := d.pickPort(sl.Ports)
		for _, ep := range sl.Endpoints {
			if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
				continue
			}
			addr := *ep.Addresses[0]
			if port != 0 {
				addr = net.JoinHostPort(addr, strconv.Itoa(int(port)))
			}
			out = append(out, Endpoint{
				Address: addr,
				Weight:  1,
			})
		}
	}
	return out
}

func (d *K8sEndpointsDiscovery) fromEndpoints(ep corev1.Endpoints) []Endpoint {
	var out []Endpoint
	for _, subset := range ep.Subsets {
		port := int32(80)
		if len(subset.Ports) > 0 {
			port = subset.Ports[0].Port
		}
		for _, addr := range subset.Addresses {
			out = append(out, Endpoint{
				Address: net.JoinHostPort(addr.IP, strconv.Itoa(int(port))),
				Weight:  1,
			})
		}
	}
	return out
}

func (d *K8sEndpointsDiscovery) pickPort(ports []discoveryv1.EndpointPort) int32 {
	for _, p := range ports {
		if p.Port != nil {
			return *p.Port
		}
	}
	return 80
}

func (d *K8sEndpointsDiscovery) Close() error { return nil }
