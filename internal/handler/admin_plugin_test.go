package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
)

func TestListPluginsIncludesConfiguredAndMarketplacePlugins(t *testing.T) {
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := "plugin-admin:plugin-secret"
	seedAdminToken(t, env, repository.RoleTenantAdmin, "plugin-admin", "plugin-secret")

	env.adminHandler.SetConfiguredPlugins(
		[]config.GRPCPluginConfig{{
			Name:    "config-router",
			Type:    "router",
			Address: "localhost:50051",
			Timeout: 100,
			Phases:  []string{"post_route"},
		}},
		[]config.WASMPluginConfig{{
			Name:        "config-auditor",
			Path:        "./missing-config-auditor.wasm",
			Phases:      []string{"audit"},
			TimeoutMs:   50,
			MemoryPages: 2,
		}},
	)

	if _, err := env.store.CreatePlugin(context.Background(), repository.CreatePluginParams{
		TenantID:    "tenant-a",
		Name:        "custom-grpc",
		Type:        "grpc",
		Phases:      []string{"post_upstream"},
		Address:     "localhost:50052",
		TimeoutMs:   50,
		MemoryPages: 1,
		Enabled:     true,
		Source:      "custom",
		Config:      map[string]any{},
	}); err != nil {
		t.Fatalf("CreatePlugin() error: %v", err)
	}

	rec := performJSONRequest(t, env, http.MethodGet, "/admin/v1/plugins", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/v1/plugins status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	items := assertSuccess(t, rec)["data"].([]any)
	if len(items) != 3 {
		t.Fatalf("GET /admin/v1/plugins items = %d, want 3: %s", len(items), rec.Body.String())
	}

	byName := map[string]map[string]any{}
	for _, item := range items {
		payload := item.(map[string]any)
		byName[payload["name"].(string)] = payload
	}
	if byName["config-router"]["source"] != "config" || byName["config-router"]["managed"] != true {
		t.Fatalf("config-router payload = %#v, want config managed plugin", byName["config-router"])
	}
	if byName["config-auditor"]["runtime_status"] != "missing_file" {
		t.Fatalf("config-auditor runtime_status = %#v, want missing_file", byName["config-auditor"]["runtime_status"])
	}
	if byName["custom-grpc"]["source"] != "custom" || byName["custom-grpc"]["managed"] != false {
		t.Fatalf("custom-grpc payload = %#v, want custom unmanaged plugin", byName["custom-grpc"])
	}
}

func TestListPluginsCanFilterConfiguredSource(t *testing.T) {
	env := newHandlerTestEnv(t, handlerTestEnvConfig{})
	adminToken := "plugin-filter-admin:plugin-filter-secret"
	seedAdminToken(t, env, repository.RoleTenantAdmin, "plugin-filter-admin", "plugin-filter-secret")
	env.adminHandler.SetConfiguredPlugins(
		[]config.GRPCPluginConfig{{Name: "config-router", Address: "localhost:50051"}},
		nil,
	)

	if _, err := env.store.CreatePlugin(context.Background(), repository.CreatePluginParams{
		TenantID: "tenant-a",
		Name:     "custom-wasm",
		Type:     "wasm",
		Phases:   []string{"audit"},
		FilePath: "./custom.wasm",
		Enabled:  true,
		Source:   "custom",
		Config:   map[string]any{},
	}); err != nil {
		t.Fatalf("CreatePlugin() error: %v", err)
	}

	rec := performJSONRequest(t, env, http.MethodGet, "/admin/v1/plugins?source=config", adminToken, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /admin/v1/plugins?source=config status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	items := assertSuccess(t, rec)["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("GET /admin/v1/plugins?source=config items = %d, want 1: %s", len(items), rec.Body.String())
	}
	payload := items[0].(map[string]any)
	if payload["name"] != "config-router" || payload["source"] != "config" {
		t.Fatalf("filtered plugin payload = %#v, want config-router", payload)
	}
}
