//go:build consul

package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConsulDiscovery_Watch(t *testing.T) {
	mockResponse := []map[string]any{
		{
			"Node": map[string]any{
				"Address": "10.0.0.1",
			},
			"Service": map[string]any{
				"Service": "my-service",
				"Address": "",
				"Port":    8080,
				"Meta": map[string]string{
					"weight": "5",
					"env":    "prod",
				},
			},
		},
		{
			"Node": map[string]any{
				"Address": "10.0.0.2",
			},
			"Service": map[string]any{
				"Service": "my-service",
				"Address": "192.168.1.10",
				"Port":    9090,
				"Meta":    map[string]string{},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health/service/my-service" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(mockResponse); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	d, err := NewConsulDiscovery(server.URL, "dc1", "")
	if err != nil {
		t.Fatalf("NewConsulDiscovery: %v", err)
	}
	defer d.Close()

	endpoints, err := d.Watch(context.Background(), "my-service")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	if len(endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(endpoints))
	}

	// First endpoint: Node.Address fallback, weight from meta
	if endpoints[0].Address != "10.0.0.1:8080" {
		t.Errorf("endpoint[0] address = %s, want 10.0.0.1:8080", endpoints[0].Address)
	}
	if endpoints[0].Weight != 5 {
		t.Errorf("endpoint[0] weight = %d, want 5", endpoints[0].Weight)
	}
	if endpoints[0].Metadata["env"] != "prod" {
		t.Errorf("endpoint[0] metadata[env] = %s, want prod", endpoints[0].Metadata["env"])
	}

	// Second endpoint: Service.Address used, default weight
	if endpoints[1].Address != "192.168.1.10:9090" {
		t.Errorf("endpoint[1] address = %s, want 192.168.1.10:9090", endpoints[1].Address)
	}
	if endpoints[1].Weight != 1 {
		t.Errorf("endpoint[1] weight = %d, want 1", endpoints[1].Weight)
	}
}

func TestConsulDiscovery_Watch_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode([]map[string]any{}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer server.Close()

	d, err := NewConsulDiscovery(server.URL, "dc1", "")
	if err != nil {
		t.Fatalf("NewConsulDiscovery: %v", err)
	}
	defer d.Close()

	endpoints, err := d.Watch(context.Background(), "unknown-service")
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(endpoints) != 0 {
		t.Fatalf("expected 0 endpoints, got %d", len(endpoints))
	}
}
