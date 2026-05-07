package discovery

import (
	"context"
	"testing"
)

func TestStaticDiscovery_Watch_Registered(t *testing.T) {
	d := NewStaticDiscovery()
	d.Register("svc1", []Endpoint{
		{Address: "10.0.0.1:8080", Weight: 1},
		{Address: "10.0.0.2:8080", Weight: 2},
	})

	eps, err := d.Watch(context.Background(), "svc1")
	if err != nil {
		t.Fatalf("Watch error: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("len = %d, want 2", len(eps))
	}
	if eps[0].Address != "10.0.0.1:8080" {
		t.Errorf("eps[0].Address = %q, want 10.0.0.1:8080", eps[0].Address)
	}
	if eps[1].Weight != 2 {
		t.Errorf("eps[1].Weight = %d, want 2", eps[1].Weight)
	}
}

func TestStaticDiscovery_Watch_DirectAddress(t *testing.T) {
	d := NewStaticDiscovery()
	eps, err := d.Watch(context.Background(), "10.0.0.1:8080, 10.0.0.2:8080")
	if err != nil {
		t.Fatalf("Watch error: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("len = %d, want 2", len(eps))
	}
	if eps[0].Address != "10.0.0.1:8080" {
		t.Errorf("eps[0].Address = %q", eps[0].Address)
	}
}

func TestStaticDiscovery_Watch_NotFound(t *testing.T) {
	d := NewStaticDiscovery()
	_, err := d.Watch(context.Background(), "   ")
	if err == nil {
		t.Error("expected error for empty service")
	}
}

func TestStaticDiscovery_Close(t *testing.T) {
	d := NewStaticDiscovery()
	if err := d.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}
}
