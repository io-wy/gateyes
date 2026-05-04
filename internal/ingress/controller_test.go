package ingress

import (
	"context"
	"fmt"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/discovery"
)

func TestController_matchesClass_ByName(t *testing.T) {
	c := &Controller{Config: configForTest("gateyes")}
	ing := ingressWithClass("gateyes")
	if !c.matchesClass(ing) {
		t.Error("expected match by ingressClassName")
	}
}

func TestController_matchesClass_ByAnnotation(t *testing.T) {
	c := &Controller{Config: configForTest("nginx")}
	ing := ingressWithAnnotation("nginx")
	if !c.matchesClass(ing) {
		t.Error("expected match by annotation")
	}
}

func TestController_matchesClass_NoMatch(t *testing.T) {
	c := &Controller{Config: configForTest("gateyes")}
	ing := ingressWithClass("nginx")
	if c.matchesClass(ing) {
		t.Error("expected no match")
	}
}

func TestController_matchesClass_EmptyClass(t *testing.T) {
	c := &Controller{Config: configForTest("")}
	ing := ingressWithClass("anything")
	if !c.matchesClass(ing) {
		t.Error("expected match when class filter is empty")
	}
}

func TestIngressRouteID(t *testing.T) {
	if got, want := ingressRouteID("ns1", "ing1"), "ns1/ing1"; got != want {
		t.Errorf("ingressRouteID = %q, want %q", got, want)
	}
}

func TestStripPort(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"10.0.0.1:8080", "10.0.0.1"},
		{"10.0.0.1", "10.0.0.1"},
		{"[::1]:8080", "[::1]"},
	}
	for _, c := range cases {
		if got := stripPort(c.in); got != c.want {
			t.Errorf("stripPort(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func configForTest(class string) config.IngressConfig {
	return config.IngressConfig{Class: class}
}

func ingressWithClass(name string) networkingv1.Ingress {
	return networkingv1.Ingress{
		Spec: networkingv1.IngressSpec{
			IngressClassName: strPtr(name),
		},
	}
}

func ingressWithAnnotation(class string) networkingv1.Ingress {
	return networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": class,
			},
		},
		Spec: networkingv1.IngressSpec{},
	}
}

func strPtr(s string) *string { return &s }

func int32Ptr(v int32) *int32 { return &v }

func newControllerForTest() *Controller {
	reg := discovery.NewRegistry("static")
	reg.Register("static", &staticDiscovery{endpoints: map[string][]discovery.Endpoint{
		"default/my-svc":   {{Address: "10.0.0.1:8080", Weight: 1}},
		"default/canary-svc": {{Address: "10.0.0.2:9090", Weight: 1}},
	}})
	return &Controller{
		Config:    configForTest("gateyes"),
		Discovery: reg,
	}
}

func TestController_ingressToRoutes(t *testing.T) {
	pathType := networkingv1.PathTypePrefix
	ing := networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-ing",
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/rewrite-target": "/v2",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "api.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/api",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "my-svc",
											Port: networkingv1.ServiceBackendPort{Number: 8080},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	c := newControllerForTest()
	routes := c.ingressToRoutes(nil, ing)

	if len(routes) != 1 {
		t.Fatalf("len(routes) = %d, want 1", len(routes))
	}
	r := routes[0]
	if r.Host != "api.example.com" {
		t.Errorf("Host = %q, want api.example.com", r.Host)
	}
	if r.Path != "/api" {
		t.Errorf("Path = %q, want /api", r.Path)
	}
	if r.Annotations == nil || r.Annotations.RewriteTarget != "/v2" {
		t.Errorf("RewriteTarget = %q, want /v2", r.Annotations.RewriteTarget)
	}
	if r.BackendPool == nil {
		t.Fatal("expected BackendPool to be set")
	}
}

func TestController_ingressToRoutes_Canary(t *testing.T) {
	pathType := networkingv1.PathTypePrefix
	ing := networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "canary-ing",
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/canary":        "true",
				"nginx.ingress.kubernetes.io/canary-weight": "20",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "api.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "canary-svc",
											Port: networkingv1.ServiceBackendPort{Number: 9090},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	c := newControllerForTest()
	routes := c.ingressToRoutes(nil, ing)

	if len(routes) != 1 {
		t.Fatalf("len(routes) = %d, want 1", len(routes))
	}
	if !routes[0].Annotations.Canary {
		t.Error("expected Canary = true")
	}
	if routes[0].Annotations.CanaryWeight != 20 {
		t.Errorf("CanaryWeight = %d, want 20", routes[0].Annotations.CanaryWeight)
	}
}

func TestController_resolveBackend_Static(t *testing.T) {
	reg := discovery.NewRegistry("static")
	reg.Register("static", &staticDiscovery{endpoints: map[string][]discovery.Endpoint{
		"default/my-svc": {{Address: "10.0.0.1:8080", Weight: 1}},
	}})

	c := &Controller{
		Config:    configForTest(""),
		Discovery: reg,
	}

	backends, err := c.resolveBackend(nil, "default/my-svc", networkingv1.ServiceBackendPort{Number: 8080})
	if err != nil {
		t.Fatalf("resolveBackend error: %v", err)
	}
	if len(backends) != 1 {
		t.Fatalf("len = %d, want 1", len(backends))
	}
	if backends[0].Address() != "10.0.0.1:8080" {
		t.Errorf("address = %q, want 10.0.0.1:8080", backends[0].Address())
	}
}

func TestController_resolveBackend_PortOverride(t *testing.T) {
	reg := discovery.NewRegistry("static")
	reg.Register("static", &staticDiscovery{endpoints: map[string][]discovery.Endpoint{
		"default/my-svc": {{Address: "10.0.0.1:8080", Weight: 1}},
	}})

	c := &Controller{
		Config:    configForTest(""),
		Discovery: reg,
	}

	// When port.Number is specified, it should override the discovered port.
	backends, err := c.resolveBackend(nil, "default/my-svc", networkingv1.ServiceBackendPort{Number: 9090})
	if err != nil {
		t.Fatalf("resolveBackend error: %v", err)
	}
	if len(backends) != 1 {
		t.Fatalf("len = %d, want 1", len(backends))
	}
	want := "10.0.0.1:9090"
	if backends[0].Address() != want {
		t.Errorf("address = %q, want %q", backends[0].Address(), want)
	}
}

// staticDiscovery is a test helper that implements discovery.ServiceDiscovery.
type staticDiscovery struct {
	endpoints map[string][]discovery.Endpoint
}

func (s *staticDiscovery) Watch(_ context.Context, serviceName string) ([]discovery.Endpoint, error) {
	eps, ok := s.endpoints[serviceName]
	if !ok {
		return nil, fmt.Errorf("service not found: %s", serviceName)
	}
	return eps, nil
}

func (s *staticDiscovery) Close() error { return nil }
