package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/service/platform"
)

func TestAdminSyncClientCreatesMissingProvider(t *testing.T) {
	var methods []string
	var payload providerSyncPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer admin:secret" {
			t.Fatalf("Authorization header = %q, want bearer token", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/v1/providers/qwen":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/admin/v1/providers":
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("Decode provider payload: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := newAdminSyncClient(server.URL, "admin:secret")
	if err != nil {
		t.Fatalf("newAdminSyncClient: %v", err)
	}
	stream := true
	err = client.SyncProvider(platform.ProviderSyncTarget{Provider: config.ProviderConfig{
		Name:         "qwen",
		Type:         "openai",
		Vendor:       "vllm",
		BaseURL:      "http://qwen.llm.svc:8000/v1",
		Endpoint:     "chat",
		Model:        "Qwen/Qwen3",
		Weight:       3,
		Enabled:      true,
		Labels:       map[string]string{"accelerator": "h100"},
		Capabilities: config.ProviderCapabilitiesConfig{Stream: &stream},
	}})
	if err != nil {
		t.Fatalf("SyncProvider: %v", err)
	}
	if got, want := methods, []string{"GET /admin/v1/providers/qwen", "POST /admin/v1/providers"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("requests = %v, want %v", got, want)
	}
	if payload.Name != "qwen" || payload.Vendor != "vllm" || payload.RoutingWeight != 3 || payload.Labels["accelerator"] != "h100" {
		t.Fatalf("provider payload = %+v, want config fields", payload)
	}
	if payload.SupportsStream == nil || !*payload.SupportsStream {
		t.Fatalf("provider capabilities = %+v, want stream override", payload)
	}
}

func TestAdminSyncClientUpdatesExistingProvider(t *testing.T) {
	var updated bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/v1/providers/existing":
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		case r.Method == http.MethodPut && r.URL.Path == "/admin/v1/providers/existing":
			updated = true
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client, err := newAdminSyncClient(server.URL, "")
	if err != nil {
		t.Fatalf("newAdminSyncClient: %v", err)
	}
	err = client.SyncProvider(platform.ProviderSyncTarget{Provider: config.ProviderConfig{Name: "existing", Model: "m", Enabled: true}})
	if err != nil {
		t.Fatalf("SyncProvider: %v", err)
	}
	if !updated {
		t.Fatal("SyncProvider did not update existing provider")
	}
}

func TestAdminSyncClientSyncsRouter(t *testing.T) {
	var payload config.RouterConfig
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/v1/sync/router" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode router payload: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client, err := newAdminSyncClient(server.URL, "")
	if err != nil {
		t.Fatalf("newAdminSyncClient: %v", err)
	}
	err = client.SyncRouter(config.RouterConfig{Strategy: "least_latency"})
	if err != nil {
		t.Fatalf("SyncRouter: %v", err)
	}
	if payload.Strategy != "least_latency" {
		t.Fatalf("router payload = %+v, want least_latency", payload)
	}
}

func TestAdminSyncClientSyncsBudget(t *testing.T) {
	var payload budgetSyncPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/v1/sync/budget" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode budget payload: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
	}))
	defer server.Close()

	client, err := newAdminSyncClient(server.URL, "")
	if err != nil {
		t.Fatalf("newAdminSyncClient: %v", err)
	}
	err = client.SyncBudget(platform.BudgetSyncTarget{
		SubjectKind:  "tenant",
		SubjectName:  "tenant-a",
		BudgetUSD:    42.5,
		BudgetPolicy: "soft_alert",
		RateLimitQPS: 7,
	})
	if err != nil {
		t.Fatalf("SyncBudget: %v", err)
	}
	if payload.SubjectKind != "tenant" || payload.BudgetUSD != 42.5 || payload.RateLimitQPS != 7 {
		t.Fatalf("budget payload = %+v, want sync target fields", payload)
	}
}
