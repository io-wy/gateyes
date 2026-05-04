package proxy

import (
	"testing"
)

func TestNewBackend_Defaults(t *testing.T) {
	b := NewBackend("b1", "10.0.0.1:8080", "", 0)
	if b.Name() != "b1" {
		t.Errorf("Name() = %q, want %q", b.Name(), "b1")
	}
	if b.Address() != "10.0.0.1:8080" {
		t.Errorf("Address() = %q, want %q", b.Address(), "10.0.0.1:8080")
	}
	if b.Weight() != 1 {
		t.Errorf("Weight() = %d, want 1", b.Weight())
	}
	if b.Protocol() != "http" {
		t.Errorf("Protocol() = %q, want http", b.Protocol())
	}
	if !b.Healthy() {
		t.Error("expected Healthy() = true")
	}
}

func TestBackend_SetHealthy(t *testing.T) {
	b := NewBackend("b1", "10.0.0.1:8080", "http", 5)
	b.SetHealthy(false)
	if b.Healthy() {
		t.Error("expected Healthy() = false after SetHealthy(false)")
	}
	b.SetHealthy(true)
	if !b.Healthy() {
		t.Error("expected Healthy() = true after SetHealthy(true)")
	}
}

func TestBackendPool_Healthy(t *testing.T) {
	b1 := NewBackend("b1", "10.0.0.1:8080", "http", 1)
	b2 := NewBackend("b2", "10.0.0.2:8080", "http", 1)
	b2.SetHealthy(false)
	b3 := NewBackend("b3", "10.0.0.3:8080", "http", 1)

	pool := NewBackendPool([]Backend{b1, b2, b3})
	healthy := pool.Healthy()
	if len(healthy) != 2 {
		t.Fatalf("Healthy() len = %d, want 2", len(healthy))
	}
	if healthy[0].Name() != "b1" || healthy[1].Name() != "b3" {
		t.Errorf("unexpected healthy backends: %v", namesOf(healthy))
	}
}

func TestBackendPool_Set(t *testing.T) {
	pool := NewBackendPool([]Backend{
		NewBackend("b1", "10.0.0.1:8080", "http", 1),
	})
	pool.Set([]Backend{
		NewBackend("b2", "10.0.0.2:8080", "http", 2),
		NewBackend("b3", "10.0.0.3:8080", "http", 3),
	})
	list := pool.List()
	if len(list) != 2 {
		t.Fatalf("List() len = %d, want 2", len(list))
	}
}

func namesOf(backends []Backend) []string {
	var out []string
	for _, b := range backends {
		out = append(out, b.Name())
	}
	return out
}
