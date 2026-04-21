package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
	"github.com/gateyes/gateway/internal/service/router"
)

func TestLiveProviderCompatibility(t *testing.T) {
	if os.Getenv("GATEYES_LIVE") != "1" {
		t.Skip("set GATEYES_LIVE=1 to run live provider compatibility checks")
	}

	env := newLiveGatewayEnv(t)
	client := env.server.Client()
	providers := selectLiveProviders(t, env.cfg, os.Getenv("GATEYES_LIVE_PROVIDERS"))
	if len(providers) == 0 {
		t.Fatal("no enabled providers selected for live test")
	}

	for _, providerCfg := range providers {
		providerCfg := providerCfg
		t.Run(providerCfg.Name, func(t *testing.T) {
			env.setTenantProviders(t, providerCfg.Name)
			t.Run("models", func(t *testing.T) {
				runLiveModelsList(t, client, env.server.URL, providerCfg)
			})
			t.Run("responses_text", func(t *testing.T) {
				runLiveResponsesText(t, client, env.server.URL, providerCfg)
			})
			t.Run("responses_stream", func(t *testing.T) {
				runLiveResponsesStream(t, client, env.server.URL, providerCfg)
			})
			t.Run("long_history", func(t *testing.T) {
				runLiveLongHistory(t, client, env.server.URL, providerCfg)
			})

			switch strings.ToLower(providerCfg.Type) {
			case "anthropic":
				t.Run("anthropic_tool_call", func(t *testing.T) {
					runLiveAnthropicToolCall(t, client, env.server.URL, providerCfg)
				})
				t.Run("anthropic_stream", func(t *testing.T) {
					runLiveAnthropicStream(t, client, env.server.URL, providerCfg)
				})
			case "grpc":
				t.Run("grpc_gateway_responses_only", func(t *testing.T) {
					runLiveGRPCGatewayResponsesOnly(t, client, env.server.URL, providerCfg)
				})
			default:
				t.Run("chat_tool_call", func(t *testing.T) {
					runLiveChatToolCall(t, client, env.server.URL, providerCfg)
				})
				t.Run("chat_stream", func(t *testing.T) {
					runLiveChatStream(t, client, env.server.URL, providerCfg)
				})
			}
		})
	}
}

func runLiveGRPCGatewayResponsesOnly(t *testing.T, client *http.Client, baseURL string, providerCfg config.ProviderConfig) {
	t.Helper()
	resp, body := doRequest(t, client, http.MethodPost, baseURL+"/v1/responses", authHeaders("live-test-key:live-test-secret"), map[string]any{
		"model":             providerCfg.Model,
		"input":             "Return one short sentence proving the gateway routed through grpc-vllm.",
		"max_output_tokens": 256,
	})
	assertStatus(t, resp, http.StatusOK, body)
	payload := decodeJSONMap(t, body)
	if payload["status"] != "completed" {
		t.Fatalf("grpc gateway responses body = %s, want completed response", body)
	}
	if text := extractResponsesText(payload); strings.TrimSpace(text) == "" {
		t.Fatalf("grpc gateway responses body = %s, want non-empty output text", body)
	}
}

func runLiveModelsList(t *testing.T, client *http.Client, baseURL string, providerCfg config.ProviderConfig) {
	t.Helper()
	resp, body := doRequest(t, client, http.MethodGet, baseURL+"/v1/models", authHeaders("live-test-key:live-test-secret"), nil)
	assertStatus(t, resp, http.StatusOK, body)
	payload := decodeJSONMap(t, body)
	models, _ := payload["data"].([]any)
	if len(models) != 1 {
		t.Fatalf("models body = %s, want exactly one visible model", body)
	}
	model, _ := models[0].(map[string]any)
	if model["id"] != providerCfg.Model || model["provider"] != providerCfg.Name || model["owned_by"] != providerCfg.Name {
		t.Fatalf("models body = %s, want model=%q provider=%q", body, providerCfg.Model, providerCfg.Name)
	}
}

func runLiveResponsesText(t *testing.T, client *http.Client, baseURL string, providerCfg config.ProviderConfig) {
	t.Helper()
	resp, body := doRequest(t, client, http.MethodPost, baseURL+"/v1/responses", authHeaders("live-test-key:live-test-secret"), map[string]any{
		"model":             providerCfg.Model,
		"input":             "Reply briefly with gateway live probe status.",
		"max_output_tokens": 512,
	})
	assertStatus(t, resp, http.StatusOK, body)
	payload := decodeJSONMap(t, body)
	if payload["status"] != "completed" {
		t.Fatalf("responses status = %#v, want completed", payload["status"])
	}
	text := extractResponsesText(payload)
	if strings.TrimSpace(text) == "" {
		t.Fatalf("responses body = %s, want non-empty output text", body)
	}
	responseID, _ := payload["id"].(string)
	if strings.TrimSpace(responseID) == "" {
		t.Fatalf("responses body = %s, want non-empty response id", body)
	}

	resp, body = doRequest(t, client, http.MethodGet, baseURL+"/v1/responses/"+responseID, authHeaders("live-test-key:live-test-secret"), nil)
	assertStatus(t, resp, http.StatusOK, body)
	stored := decodeJSONMap(t, body)
	if stored["id"] != responseID || stored["status"] != "completed" {
		t.Fatalf("stored response body = %s, want matching completed response", body)
	}
	if stored["model"] != providerCfg.Model {
		t.Fatalf("stored response body = %s, want model %q", body, providerCfg.Model)
	}
	if storedText := extractResponsesText(stored); strings.TrimSpace(storedText) == "" {
		t.Fatalf("stored response body = %s, want non-empty output text", body)
	}
}

func runLiveResponsesStream(t *testing.T, client *http.Client, baseURL string, providerCfg config.ProviderConfig) {
	t.Helper()
	resp, body := doRequest(t, client, http.MethodPost, baseURL+"/v1/responses", authHeaders("live-test-key:live-test-secret"), map[string]any{
		"model":             providerCfg.Model,
		"input":             "Stream a short status sentence.",
		"max_output_tokens": 512,
		"stream":            true,
	})
	assertStatus(t, resp, http.StatusOK, body)
	events := parseSSEData(body)
	if len(events) < 2 || events[len(events)-1] != "[DONE]" {
		t.Fatalf("responses stream events = %v, want done-terminated SSE", events)
	}
	if containsSSEError(body) {
		t.Fatalf("responses stream body = %s, want no SSE error event", body)
	}
	types := collectTypes(t, events[:len(events)-1])
	if !contains(types, "response.completed") {
		t.Fatalf("responses stream events = %v, want response.completed", events)
	}
	if !contains(types, "response.output_text.delta") && !contains(types, "response.output_item.done") {
		t.Fatalf("responses stream events = %v, want visible text or tool output events", events)
	}
}

func runLiveLongHistory(t *testing.T, client *http.Client, baseURL string, providerCfg config.ProviderConfig) {
	t.Helper()
	messages := make([]map[string]any, 0, 41)
	for i := 0; i < 20; i++ {
		messages = append(messages,
			map[string]any{"role": "user", "content": fmt.Sprintf("memory turn %d user", i)},
			map[string]any{"role": "assistant", "content": fmt.Sprintf("memory turn %d assistant", i)},
		)
	}
	messages = append(messages, map[string]any{
		"role":    "user",
		"content": "Summarize the pattern in one short sentence.",
	})

	resp, body := doRequest(t, client, http.MethodPost, baseURL+"/v1/responses", authHeaders("live-test-key:live-test-secret"), map[string]any{
		"model":             providerCfg.Model,
		"messages":          messages,
		"max_output_tokens": 512,
	})
	assertStatus(t, resp, http.StatusOK, body)
	payload := decodeJSONMap(t, body)
	if text := extractResponsesText(payload); strings.TrimSpace(text) == "" {
		t.Fatalf("long history body = %s, want non-empty output text", body)
	}
}

func runLiveChatToolCall(t *testing.T, client *http.Client, baseURL string, providerCfg config.ProviderConfig) {
	t.Helper()
	resp, body := doRequest(t, client, http.MethodPost, baseURL+"/v1/chat/completions", authHeaders("live-test-key:live-test-secret"), map[string]any{
		"model": providerCfg.Model,
		"messages": []map[string]any{{
			"role":    "system",
			"content": "You must call the provided tool before answering.",
		}, {
			"role":    "user",
			"content": "Check gateway status.",
		}},
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "get_probe_status",
				"description": "Return gateway probe status",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"topic": map[string]any{"type": "string"},
					},
					"required": []string{"topic"},
				},
			},
		}},
		"max_tokens": 512,
	})
	assertStatus(t, resp, http.StatusOK, body)
	payload := decodeJSONMap(t, body)
	choice := payload["choices"].([]any)[0].(map[string]any)
	message := choice["message"].(map[string]any)
	toolCalls, _ := message["tool_calls"].([]any)
	if choice["finish_reason"] != "tool_calls" || len(toolCalls) == 0 {
		t.Fatalf("chat tool call body = %s, want tool_calls output", body)
	}
	function, _ := toolCalls[0].(map[string]any)["function"].(map[string]any)
	if function["name"] != "get_probe_status" {
		t.Fatalf("chat tool call body = %s, want get_probe_status call", body)
	}
	arguments, _ := function["arguments"].(string)
	if !strings.Contains(arguments, "topic") {
		t.Fatalf("chat tool call body = %s, want tool arguments with topic", body)
	}
}

func runLiveChatStream(t *testing.T, client *http.Client, baseURL string, providerCfg config.ProviderConfig) {
	t.Helper()
	resp, body := doRequest(t, client, http.MethodPost, baseURL+"/v1/chat/completions", authHeaders("live-test-key:live-test-secret"), map[string]any{
		"model": providerCfg.Model,
		"messages": []map[string]any{{
			"role":    "system",
			"content": "You must call the provided tool before answering.",
		}, {
			"role":    "user",
			"content": "Check gateway status.",
		}},
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "get_probe_status",
				"description": "Return gateway probe status",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"topic": map[string]any{"type": "string"},
					},
					"required": []string{"topic"},
				},
			},
		}},
		"stream":     true,
		"max_tokens": 512,
	})
	assertStatus(t, resp, http.StatusOK, body)
	events := parseSSEData(body)
	if len(events) < 2 || events[len(events)-1] != "[DONE]" {
		t.Fatalf("chat stream events = %v, want done-terminated SSE", events)
	}
	if containsSSEError(body) {
		t.Fatalf("chat stream body = %s, want no SSE error event", body)
	}
	snippets := bodyJSONSnippets(events[:len(events)-1])
	if !contains(snippets, `"role":"assistant"`) || !contains(snippets, `"tool_calls"`) || !contains(snippets, `"finish_reason":"tool_calls"`) {
		t.Fatalf("chat stream body = %s, want assistant role, tool_calls and tool_calls finish", body)
	}
}

func runLiveAnthropicToolCall(t *testing.T, client *http.Client, baseURL string, providerCfg config.ProviderConfig) {
	t.Helper()
	headers := authHeaders("bad:bad")
	headers["X-Api-Key"] = "live-test-key:live-test-secret"
	resp, body := doRequest(t, client, http.MethodPost, baseURL+"/v1/messages", headers, map[string]any{
		"model": providerCfg.Model,
		"messages": []map[string]any{{
			"role":    "user",
			"content": "You must call get_probe_status for topic gateway before answering.",
		}},
		"tools": []map[string]any{{
			"name":        "get_probe_status",
			"description": "Return gateway probe status",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"topic": map[string]any{"type": "string"},
				},
				"required": []string{"topic"},
			},
		}},
		"max_tokens": 512,
	})
	assertStatus(t, resp, http.StatusOK, body)
	payload := decodeJSONMap(t, body)
	if payload["stop_reason"] != "tool_use" {
		t.Fatalf("anthropic tool call body = %s, want stop_reason tool_use", body)
	}
	content, _ := payload["content"].([]any)
	if !containsAnthropicToolUse(content, "get_probe_status") {
		t.Fatalf("anthropic tool call body = %s, want get_probe_status tool_use block", body)
	}
}

func runLiveAnthropicStream(t *testing.T, client *http.Client, baseURL string, providerCfg config.ProviderConfig) {
	t.Helper()
	headers := authHeaders("bad:bad")
	headers["X-Api-Key"] = "live-test-key:live-test-secret"
	resp, body := doRequest(t, client, http.MethodPost, baseURL+"/v1/messages", headers, map[string]any{
		"model": providerCfg.Model,
		"messages": []map[string]any{{
			"role":    "user",
			"content": "You must call get_probe_status for topic gateway before answering.",
		}},
		"tools": []map[string]any{{
			"name":        "get_probe_status",
			"description": "Return gateway probe status",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"topic": map[string]any{"type": "string"},
				},
				"required": []string{"topic"},
			},
		}},
		"max_tokens": 512,
		"stream":     true,
	})
	assertStatus(t, resp, http.StatusOK, body)
	events := parseSSEData(body)
	if len(events) < 2 || events[len(events)-1] != "[DONE]" {
		t.Fatalf("anthropic stream events = %v, want done-terminated SSE", events)
	}
	if containsSSEError(body) {
		t.Fatalf("anthropic stream body = %s, want no SSE error event", body)
	}
	snippets := bodyJSONSnippets(events[:len(events)-1])
	if !contains(snippets, `"type":"message_start"`) || !contains(snippets, `"type":"message_stop"`) || !contains(snippets, `"type":"tool_use"`) || !contains(snippets, `"stop_reason":"tool_use"`) {
		t.Fatalf("anthropic stream body = %s, want message_start/message_stop/tool_use/stop_reason", body)
	}
}

func newLiveGatewayEnv(t *testing.T) *gatewayE2EEnv {
	t.Helper()

	cfgPath := os.Getenv("GATEYES_LIVE_CONFIG")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}

	cfgObj, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config %s: %v", cfgPath, err)
	}

	ctx := context.Background()
	database, err := db.Open(config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "gateway-live.db"),
		AutoMigrate:            true,
		MaxOpenConns:           4,
		MaxIdleConns:           4,
		ConnMaxLifetimeSeconds: 60,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	store := sqlstore.New(database)
	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID:     "tenant-live",
		Slug:   "tenant-live",
		Name:   "tenant-live",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenant.ID,
		Key:        "live-test-key",
		SecretHash: repository.HashSecret("live-test-secret"),
		Name:       "live-test-user",
		Email:      "live@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      1000000,
		QPS:        50,
	}); err != nil {
		t.Fatalf("seed live api key: %v", err)
	}

	cfgObj.Database = config.DatabaseConfig{}
	cfgObj.Server.ListenAddr = ":0"
	cfgObj.Metrics.Namespace = fmt.Sprintf("gateway_live_%d", time.Now().UnixNano())

	providerMgr, err := provider.NewManager(cfgObj.Providers)
	if err != nil {
		t.Fatalf("new provider manager: %v", err)
	}
	routerSvc := router.NewRouter(cfgObj.Router)
	routerSvc.SetProviders(providerMgr.List())
	limiterSvc := limiter.NewLimiter(config.LimiterConfig{
		GlobalQPS:           100,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000000,
		PerUserRequestBurst: 100,
		QueueSize:           128,
	})
	t.Cleanup(limiterSvc.Stop)

	metrics := NewMetrics(cfgObj.Metrics.Namespace)
	mw := middleware.New(store, limiterSvc, metrics)
	responseService := responseSvc.New(&responseSvc.Dependencies{
		Config:      cfgObj,
		Store:       store,
		Auth:        mw.AuthService(),
		ProviderMgr: providerMgr,
		Router:      routerSvc,
		Alert:       nil,
	})
	catalogSvc := catalog.New(&catalog.Dependencies{
		Store:     store,
		Auth:      mw.AuthService(),
		Limiter:   limiterSvc,
		Responses: responseService,
	})
	h := NewHandler(&Dependencies{
		Config:      cfgObj,
		Store:       store,
		Metrics:     metrics,
		ProviderMgr: providerMgr,
		ResponseSvc: responseService,
		CatalogSvc:  catalogSvc,
	})
	adminHandler := NewAdminHandler(store, providerMgr, catalogSvc)
	handlerEnv := &handlerTestEnv{
		server:      NewServer(cfgObj.Server, h, adminHandler, mw),
		store:       store,
		providerMgr: providerMgr,
	}

	env := &gatewayE2EEnv{
		handlerEnv: handlerEnv,
		server:     httptest.NewServer(handlerEnv.server.engine),
		cfg:        cfgObj,
		tenantID:   tenant.ID,
	}
	t.Cleanup(env.server.Close)
	return env
}

func selectLiveProviders(t *testing.T, cfg *config.Config, selected string) []config.ProviderConfig {
	t.Helper()
	if cfg == nil {
		return nil
	}
	enabled := make([]config.ProviderConfig, 0, len(cfg.Providers))
	index := make(map[string]config.ProviderConfig, len(cfg.Providers))
	for _, providerCfg := range cfg.Providers {
		if !providerCfg.Enabled {
			continue
		}
		enabled = append(enabled, providerCfg)
		index[providerCfg.Name] = providerCfg
	}
	if strings.TrimSpace(selected) == "" {
		return enabled
	}

	var result []config.ProviderConfig
	for _, name := range strings.Split(selected, ",") {
		name = strings.TrimSpace(name)
		providerCfg, ok := index[name]
		if !ok {
			t.Fatalf("selected live provider %q not found or not enabled", name)
		}
		result = append(result, providerCfg)
	}
	return result
}

func extractResponsesText(payload map[string]any) string {
	output, _ := payload["output"].([]any)
	var builder strings.Builder
	for _, item := range output {
		itemMap, _ := item.(map[string]any)
		content, _ := itemMap["content"].([]any)
		for _, block := range content {
			blockMap, _ := block.(map[string]any)
			if text, _ := blockMap["text"].(string); text != "" {
				builder.WriteString(text)
			}
		}
	}
	return builder.String()
}

func containsSSEError(body []byte) bool {
	text := string(body)
	return strings.Contains(text, `"type":"error"`) || strings.Contains(text, `"error":`)
}

func containsAnthropicToolUse(content []any, wantName string) bool {
	for _, block := range content {
		blockMap, _ := block.(map[string]any)
		if blockMap["type"] != "tool_use" {
			continue
		}
		if wantName != "" && blockMap["name"] != wantName {
			continue
		}
		return true
	}
	return false
}
