package discovery

import (
	"context"
	"testing"
)

type mockDiscovery struct {
	endpoints []Endpoint
	err       error
}

func (m *mockDiscovery) Watch(_ context.Context, _ string) ([]Endpoint, error) {
	return m.endpoints, m.err
}

func (m *mockDiscovery) Close() error { return nil }

func TestRegistry_RegisterAndDiscover(t *testing.T) {
	r := NewRegistry("mock")
	mock := &mockDiscovery{endpoints: []Endpoint{{Address: "1.2.3.4:80", Weight: 1}}}
	r.Register("mock", mock)

	eps, err := r.Discover(context.Background(), "mock", "svc1")
	if err != nil {
		t.Fatalf("Discover error: %v", err)
	}
	if len(eps) != 1 || eps[0].Address != "1.2.3.4:80" {
		t.Errorf("unexpected endpoints: %+v", eps)
	}
}

func TestRegistry_DefaultType(t *testing.T) {
	r := NewRegistry("")
	if r.DefaultType() != "static" {
		t.Errorf("DefaultType() = %q, want static", r.DefaultType())
	}
}

func TestRegistry_UnknownType(t *testing.T) {
	r := NewRegistry("static")
	_, err := r.Discover(context.Background(), "unknown", "svc1")
	if err == nil {
		t.Error("expected error for unknown discovery type")
	}
}
