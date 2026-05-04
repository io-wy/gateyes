package ingress

import (
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/gateyes/gateway/internal/config"
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
