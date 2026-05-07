package discovery

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newFakeClient(objs ...client.Object) client.Reader {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = discoveryv1.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func TestK8sEndpointsDiscovery_FromEndpoints(t *testing.T) {
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "my-service",
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{IP: "10.0.0.1"},
					{IP: "10.0.0.2"},
				},
				Ports: []corev1.EndpointPort{
					{Port: 8080},
				},
			},
		},
	}
	c := newFakeClient(endpoints)
	d := NewK8sEndpointsDiscovery(c, "default")

	eps, err := d.Watch(context.Background(), "default/my-service")
	if err != nil {
		t.Fatalf("Watch error: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("len = %d, want 2", len(eps))
	}
	if eps[0].Address != "10.0.0.1:8080" {
		t.Errorf("eps[0].Address = %q, want 10.0.0.1:8080", eps[0].Address)
	}
	if eps[1].Address != "10.0.0.2:8080" {
		t.Errorf("eps[1].Address = %q, want 10.0.0.2:8080", eps[1].Address)
	}
}

func TestK8sEndpointsDiscovery_FromEndpointSlices(t *testing.T) {
	ready := true
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "svc-abc",
			Labels:    map[string]string{"kubernetes.io/service-name": "my-service"},
		},
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses:  []string{"10.0.0.3"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			},
			{
				Addresses:  []string{"10.0.0.4"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			},
		},
		Ports: []discoveryv1.EndpointPort{
			{Port: int32Ptr(9090)},
		},
	}
	c := newFakeClient(slice)
	d := NewK8sEndpointsDiscovery(c, "default")

	eps, err := d.Watch(context.Background(), "default/my-service")
	if err != nil {
		t.Fatalf("Watch error: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("len = %d, want 2", len(eps))
	}
	if eps[0].Address != "10.0.0.3:9090" {
		t.Errorf("eps[0].Address = %q, want 10.0.0.3:9090", eps[0].Address)
	}
}

func TestK8sEndpointsDiscovery_NotReadySkipped(t *testing.T) {
	ready := false
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "svc-abc",
			Labels:    map[string]string{"kubernetes.io/service-name": "my-svc"},
		},
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses:  []string{"10.0.0.5"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			},
		},
		Ports: []discoveryv1.EndpointPort{{Port: int32Ptr(80)}},
	}
	c := newFakeClient(slice)
	d := NewK8sEndpointsDiscovery(c, "default")

	eps, err := d.Watch(context.Background(), "default/my-svc")
	if err != nil {
		t.Fatalf("Watch error: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("len = %d, want 0 (not ready skipped)", len(eps))
	}
}

func int32Ptr(v int32) *int32 { return &v }
