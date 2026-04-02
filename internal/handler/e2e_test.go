package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
	"github.com/gateyes/gateway/internal/service/router"
)

func TestGatewayE2E(t *testing.T) {
	gin.SetMode(gin.TestMode)

	env := newGatewayE2EEnv(t)
	client := env.server.Client()

	seedAdminToken(t, env.handlerEnv, repository.RoleTenantAdmin, "e2e-tenant-admin", "tenant-admin-secret")
	adminToken := "e2e-tenant-admin:tenant-admin-secret"
	seedAdminToken(t, env.handlerEnv, repository.RoleSuperAdmin, "e2e-super-admin", "super-admin-secret")
	superToken := "e2e-super-admin:super-admin-secret"

	t.Run("health ready and invalid auth", func(t *testing.T) {
		resp, body := doRequest(t, client, http.MethodGet, env.server.URL+"/health", nil, nil)
		assertStatus(t, resp, http.StatusOK, body)
		if !strings.Contains(string(body), `"status":"ok"`) {
			t.Fatalf("GET /health body = %s, want ok status", body)
		}

		resp, body = doRequest(t, client, http.MethodGet, env.server.URL+"/ready", nil, nil)
		assertStatus(t, resp, http.StatusOK, body)
		if !strings.Contains(string(body), `"status":"ready"`) {
			t.Fatalf("GET /ready body = %s, want ready status", body)
		}

		resp, body = doRequest(t, client, http.MethodPost, env.server.URL+"/v1/chat/completions", map[string]string{
			"Authorization": "Bearer invalid-key:invalid-secret",
			"Content-Type":  "application/json",
		}, map[string]any{
			"model": "chat-public",
			"messages": []map[string]any{{
				"role":    "user",
				"content": "hello",
			}},
		})
		assertStatus(t, resp, http.StatusUnauthorized, body)
		if !strings.Contains(string(body), "invalid API key") {
			t.Fatalf("invalid auth body = %s, want invalid API key", body)
		}
	})

	t.Run("responses create and retrieve", func(t *testing.T) {
		env.setTenantProviders(t, "openai-responses")

		resp, body := doRequest(t, client, http.MethodPost, env.server.URL+"/v1/responses", authHeaders("test-key:test-secret"), map[string]any{
			"model":             "resp-public",
			"input":             "say hi",
			"max_output_tokens": 64,
		})
		assertStatus(t, resp, http.StatusOK, body)
		payload := decodeJSONMap(t, body)
		if payload["model"] != "resp-public" || payload["status"] != "completed" {
			t.Fatalf("POST /v1/responses payload = %#v, want normalized response", payload)
		}
		output := payload["output"].([]any)
		first := output[0].(map[string]any)
		content := first["content"].([]any)[0].(map[string]any)
		if content["text"] != "response hello" {
			t.Fatalf("POST /v1/responses text = %v, want %q", content["text"], "response hello")
		}

		responseID := payload["id"].(string)
		resp, body = doRequest(t, client, http.MethodGet, env.server.URL+"/v1/responses/"+responseID, authHeaders("test-key:test-secret"), nil)
		assertStatus(t, resp, http.StatusOK, body)
		stored := decodeJSONMap(t, body)
		if stored["id"] != responseID || stored["model"] != "resp-public" {
			t.Fatalf("GET /v1/responses/:id payload = %#v, want stored response body", stored)
		}
	})

	t.Run("chat compatibility and tool calls", func(t *testing.T) {
		env.setTenantProviders(t, "openai-chat")

		resp, body := doRequest(t, client, http.MethodPost, env.server.URL+"/v1/chat/completions", authHeaders("test-key:test-secret"), map[string]any{
			"model": "chat-public",
			"messages": []map[string]any{
				{"role": "user", "content": "My name is Alice"},
				{"role": "assistant", "content": "Hello Alice"},
				{"role": "user", "content": "What is my name?"},
			},
			"max_tokens": 64,
		})
		assertStatus(t, resp, http.StatusOK, body)
		payload := decodeJSONMap(t, body)
		choice := payload["choices"].([]any)[0].(map[string]any)
		message := choice["message"].(map[string]any)
		if message["content"] != "Your name is Alice." {
			t.Fatalf("chat content = %v, want Alice answer", message["content"])
		}

		resp, body = doRequest(t, client, http.MethodPost, env.server.URL+"/v1/chat/completions", authHeaders("test-key:test-secret"), map[string]any{
			"model": "chat-public",
			"messages": []map[string]any{{
				"role":    "user",
				"content": "call a tool",
			}},
			"tools": []map[string]any{{
				"type": "function",
				"function": map[string]any{
					"name":        "lookup",
					"description": "lookup weather",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
					},
				},
			}},
		})
		assertStatus(t, resp, http.StatusOK, body)
		payload = decodeJSONMap(t, body)
		choice = payload["choices"].([]any)[0].(map[string]any)
		message = choice["message"].(map[string]any)
		if choice["finish_reason"] != "tool_calls" {
			t.Fatalf("chat finish_reason = %v, want tool_calls", choice["finish_reason"])
		}
		toolCalls := message["tool_calls"].([]any)
		fn := toolCalls[0].(map[string]any)["function"].(map[string]any)
		if fn["name"] != "lookup" {
			t.Fatalf("chat tool name = %v, want lookup", fn["name"])
		}
	})

	t.Run("anthropic compatibility and x-api-key precedence", func(t *testing.T) {
		env.setTenantProviders(t, "anthropic-main")

		headers := authHeaders("wrong:wrong")
		headers["X-Api-Key"] = "test-key:test-secret"

		resp, body := doRequest(t, client, http.MethodPost, env.server.URL+"/v1/messages", headers, map[string]any{
			"model": "anthropic-public",
			"messages": []map[string]any{{
				"role":    "user",
				"content": "hello",
			}},
			"max_tokens": 64,
			"tools": []map[string]any{{
				"name":         "lookup",
				"description":  "lookup weather",
				"input_schema": map[string]any{"type": "object"},
			}},
		})
		assertStatus(t, resp, http.StatusOK, body)
		payload := decodeJSONMap(t, body)
		if payload["type"] != "message" || payload["stop_reason"] != "tool_use" {
			t.Fatalf("POST /v1/messages payload = %#v, want anthropic message with tool_use stop", payload)
		}
		blocks := payload["content"].([]any)
		if blocks[0].(map[string]any)["text"] != "anthropic hello" {
			t.Fatalf("anthropic first block = %#v, want text block", blocks[0])
		}
		if blocks[1].(map[string]any)["type"] != "tool_use" {
			t.Fatalf("anthropic second block = %#v, want tool_use block", blocks[1])
		}
	})

	t.Run("models and admin flows", func(t *testing.T) {
		env.setTenantProviders(t, "openai-chat", "openai-responses", "anthropic-main")

		resp, body := doRequest(t, client, http.MethodGet, env.server.URL+"/v1/models", authHeaders("test-key:test-secret"), nil)
		assertStatus(t, resp, http.StatusOK, body)
		payload := decodeJSONMap(t, body)
		models := payload["data"].([]any)
		if len(models) != 3 {
			t.Fatalf("/v1/models count = %d, want 3", len(models))
		}

		resp, body = doRequest(t, client, http.MethodGet, env.server.URL+"/admin/providers", authHeaders(adminToken), nil)
		assertStatus(t, resp, http.StatusOK, body)
		payload = decodeJSONMap(t, body)
		providers := payload["data"].([]any)
		if len(providers) != 3 {
			t.Fatalf("/admin/providers count = %d, want 3", len(providers))
		}

		resp, body = doRequest(t, client, http.MethodGet, env.server.URL+"/admin/providers/openai-chat/stats", authHeaders(adminToken), nil)
		assertStatus(t, resp, http.StatusOK, body)
		payload = decodeJSONMap(t, body)
		if payload["data"].(map[string]any)["name"] != "openai-chat" {
			t.Fatalf("/admin/providers/:name/stats payload = %#v, want openai-chat", payload)
		}

		resp, body = doRequest(t, client, http.MethodPost, env.server.URL+"/admin/users", authHeaders(adminToken), map[string]any{
			"name":   "e2e-user",
			"email":  "e2e@example.com",
			"models": []string{"chat-public", "resp-public"},
		})
		assertStatus(t, resp, http.StatusCreated, body)
		payload = decodeJSONMap(t, body)
		user := payload["data"].(map[string]any)
		userToken := user["api_key"].(string) + ":" + user["api_secret"].(string)

		resp, body = doRequest(t, client, http.MethodGet, env.server.URL+"/v1/models", authHeaders(userToken), nil)
		assertStatus(t, resp, http.StatusOK, body)

		resp, body = doRequest(t, client, http.MethodGet, env.server.URL+"/admin/providers", authHeaders(userToken), nil)
		assertStatus(t, resp, http.StatusForbidden, body)

		resp, body = doRequest(t, client, http.MethodGet, env.server.URL+"/admin/tenants", authHeaders(superToken), nil)
		assertStatus(t, resp, http.StatusOK, body)
	})

	t.Run("responses streaming compatibility", func(t *testing.T) {
		env.setTenantProviders(t, "openai-responses")

		resp, body := doRequest(t, client, http.MethodPost, env.server.URL+"/v1/responses", authHeaders("test-key:test-secret"), map[string]any{
			"model":             "resp-public",
			"input":             "stream please",
			"max_output_tokens": 64,
			"stream":            true,
			"tools": []map[string]any{{
				"type": "function",
				"function": map[string]any{
					"name":       "lookup",
					"parameters": map[string]any{"type": "object"},
				},
			}},
		})
		assertStatus(t, resp, http.StatusOK, body)
		events := parseSSEData(body)
		if len(events) < 4 || events[len(events)-1] != "[DONE]" {
			t.Fatalf("responses stream events = %v, want done-terminated SSE", events)
		}
		responseTypes := collectTypes(t, events[:len(events)-1])
		if !contains(responseTypes, "response.created") || !contains(responseTypes, "response.output_text.delta") || !contains(responseTypes, "response.completed") {
			t.Fatalf("responses stream types = %v, want created/output_text.delta/completed", responseTypes)
		}
		if !contains(bodyJSONSnippets(events[:len(events)-1]), `"type":"function_call"`) {
			t.Fatalf("responses stream body = %s, want function_call in completed payload", body)
		}
	})

	t.Run("chat streaming compatibility", func(t *testing.T) {
		env.setTenantProviders(t, "openai-chat")

		resp, body := doRequest(t, client, http.MethodPost, env.server.URL+"/v1/chat/completions", authHeaders("test-key:test-secret"), map[string]any{
			"model": "chat-public",
			"messages": []map[string]any{{
				"role":    "user",
				"content": "stream a tool call",
			}},
			"stream": true,
			"tools": []map[string]any{{
				"type": "function",
				"function": map[string]any{
					"name":       "lookup",
					"parameters": map[string]any{"type": "object"},
				},
			}},
		})
		assertStatus(t, resp, http.StatusOK, body)
		events := parseSSEData(body)
		if len(events) < 3 || events[len(events)-1] != "[DONE]" {
			t.Fatalf("chat stream events = %v, want done-terminated SSE", events)
		}
		if !contains(bodyJSONSnippets(events[:len(events)-1]), `"role":"assistant"`) || !contains(bodyJSONSnippets(events[:len(events)-1]), `"tool_calls"`) || !contains(bodyJSONSnippets(events[:len(events)-1]), `"finish_reason":"tool_calls"`) {
			t.Fatalf("chat stream body = %s, want assistant role, tool_calls and tool_calls finish", body)
		}
	})

	t.Run("anthropic streaming compatibility", func(t *testing.T) {
		env.setTenantProviders(t, "anthropic-main")

		headers := authHeaders("bad:bad")
		headers["X-Api-Key"] = "test-key:test-secret"
		resp, body := doRequest(t, client, http.MethodPost, env.server.URL+"/v1/messages", headers, map[string]any{
			"model": "anthropic-public",
			"messages": []map[string]any{{
				"role":    "user",
				"content": "stream anthropic",
			}},
			"max_tokens": 64,
			"stream":     true,
			"tools": []map[string]any{{
				"name":         "lookup",
				"description":  "lookup weather",
				"input_schema": map[string]any{"type": "object"},
			}},
		})
		assertStatus(t, resp, http.StatusOK, body)
		events := parseSSEData(body)
		if len(events) < 6 || events[len(events)-1] != "[DONE]" {
			t.Fatalf("anthropic stream events = %v, want done-terminated SSE", events)
		}
		anthropicTypes := collectTypes(t, events[:len(events)-1])
		if !contains(anthropicTypes, "message_start") || !contains(anthropicTypes, "content_block_delta") || !contains(anthropicTypes, "message_stop") {
			t.Fatalf("anthropic stream types = %v, want message_start/content_block_delta/message_stop", anthropicTypes)
		}
		if !contains(bodyJSONSnippets(events[:len(events)-1]), `"type":"tool_use"`) {
			t.Fatalf("anthropic stream body = %s, want tool_use block", body)
		}
	})
}

type gatewayE2EEnv struct {
	handlerEnv *handlerTestEnv
	server     *httptest.Server
}

func (e *gatewayE2EEnv) setTenantProviders(t *testing.T, names ...string) {
	t.Helper()
	if err := e.handlerEnv.store.ReplaceTenantProviders(context.Background(), "tenant-a", names); err != nil {
		t.Fatalf("ReplaceTenantProviders(%v): %v", names, err)
	}
}

func newGatewayE2EEnv(t *testing.T) *gatewayE2EEnv {
	t.Helper()

	upstream := newGatewayE2EUpstream(t)
	t.Cleanup(upstream.Close)

	ctx := context.Background()
	database, err := db.Open(config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "gateway-e2e.db"),
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
		ID:     "tenant-a",
		Slug:   "tenant-a",
		Name:   "tenant-a",
		Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	if err := store.ReplaceTenantProviders(ctx, tenant.ID, []string{"openai-chat", "openai-responses", "anthropic-main"}); err != nil {
		t.Fatalf("replace tenant providers: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenant.ID,
		Key:        "test-key",
		SecretHash: repository.HashSecret("test-secret"),
		Name:       "test-user",
		Email:      "test@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      100000,
		QPS:        100,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	cfgObj := &config.Config{
		Server: config.ServerConfig{ListenAddr: ":0", ReadTimeout: 30, WriteTimeout: 300},
		Metrics: config.MetricsConfig{
			Namespace: fmt.Sprintf("gateway_e2e_%d", time.Now().UnixNano()),
		},
		Router: config.RouterConfig{Strategy: "round_robin"},
		Providers: []config.ProviderConfig{
			{Name: "openai-chat", Type: "openai", BaseURL: upstream.URL, Endpoint: "chat", APIKey: "upstream-key", Model: "chat-public", Timeout: 5, Enabled: true, MaxTokens: 256},
			{Name: "openai-responses", Type: "openai", BaseURL: upstream.URL, Endpoint: "responses", APIKey: "upstream-key", Model: "resp-public", Timeout: 5, Enabled: true, MaxTokens: 256},
			{Name: "anthropic-main", Type: "anthropic", BaseURL: upstream.URL, APIKey: "anthropic-key", Model: "anthropic-public", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	}

	metrics := NewMetrics(cfgObj.Metrics.Namespace)
	providerMgr, err := provider.NewManager(cfgObj.Providers)
	if err != nil {
		t.Fatalf("new provider manager: %v", err)
	}
	routerSvc := router.NewRouter(cfgObj.Router)
	routerSvc.SetProviders(providerMgr.List())
	limiterSvc := limiter.NewLimiter(config.LimiterConfig{
		GlobalQPS:           100,
		GlobalTPM:           100000,
		GlobalTokenBurst:    100000,
		PerUserRequestBurst: 100,
		QueueSize:           128,
	})
	t.Cleanup(limiterSvc.Stop)
	mw := middleware.New(store, limiterSvc)
	responseService := responseSvc.New(&responseSvc.Dependencies{
		Config:      cfgObj,
		Store:       store,
		Auth:        mw.AuthService(),
		ProviderMgr: providerMgr,
		Router:      routerSvc,
		Alert:       nil,
	})
	h := NewHandler(&Dependencies{
		Config:      cfgObj,
		Store:       store,
		Metrics:     metrics,
		ProviderMgr: providerMgr,
		ResponseSvc: responseService,
	})
	adminHandler := NewAdminHandler(store, providerMgr)
	handlerEnv := &handlerTestEnv{
		server:      NewServer(cfgObj.Server, h, adminHandler, mw),
		store:       store,
		providerMgr: providerMgr,
	}

	env := &gatewayE2EEnv{
		handlerEnv: handlerEnv,
		server:     httptest.NewServer(handlerEnv.server.engine),
	}
	t.Cleanup(env.server.Close)
	return env
}

func newGatewayE2EUpstream(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		defer r.Body.Close()
		payload := decodeMaybeJSON(body)
		stream := payload["stream"] == true
		hasTools := len(anySlice(payload["tools"])) > 0

		switch r.URL.Path {
		case "/responses":
			if got := r.Header.Get("Authorization"); got != "Bearer upstream-key" {
				t.Fatalf("responses auth = %q, want Bearer upstream-key", got)
			}
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"response stream\"}\n\n")
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"call-resp-1\",\"type\":\"function_call\",\"name\":\"lookup\",\"arguments\":\"{\\\"city\\\":\\\"Shanghai\\\"}\"}}\n\n")
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"up-resp\",\"created_at\":1,\"model\":\"resp-public\",\"status\":\"completed\",\"output\":[{\"id\":\"msg-1\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"response stream\"}]},{\"id\":\"call-resp-1\",\"type\":\"function_call\",\"call_id\":\"call-resp-1\",\"name\":\"lookup\",\"arguments\":\"{\\\"city\\\":\\\"Shanghai\\\"}\"}],\"usage\":{\"input_tokens\":4,\"output_tokens\":6,\"total_tokens\":10}}}\n\n")
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "up-resp",
				"created_at": 1,
				"model":      "resp-public",
				"status":     "completed",
				"output": []map[string]any{{
					"id":     "msg-1",
					"type":   "message",
					"role":   "assistant",
					"status": "completed",
					"content": []map[string]any{{
						"type": "output_text",
						"text": "response hello",
					}},
				}},
				"usage": map[string]any{"input_tokens": 3, "output_tokens": 2, "total_tokens": 5},
			})
		case "/v1/chat/completions":
			if got := r.Header.Get("Authorization"); got != "Bearer upstream-key" {
				t.Fatalf("chat auth = %q, want Bearer upstream-key", got)
			}
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-stream-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"chat-public\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-chat-1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"city\\\":\\\"Shanghai\\\"}\"}}]}}]}\n\n")
				_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-stream-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"chat-public\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\n")
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
				return
			}
			if hasTools {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":      "chat-tool-1",
					"object":  "chat.completion",
					"created": 1,
					"model":   "chat-public",
					"choices": []map[string]any{{
						"index": 0,
						"message": map[string]any{
							"role":    "assistant",
							"content": "",
							"tool_calls": []map[string]any{{
								"id":   "call-chat-1",
								"type": "function",
								"function": map[string]any{
									"name":      "lookup",
									"arguments": "{\"city\":\"Shanghai\"}",
								},
							}},
						},
						"finish_reason": "tool_calls",
					}},
					"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
				})
				return
			}
			content := "chat hello"
			if bytes.Contains(body, []byte("What is my name?")) {
				content = "Your name is Alice."
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chat-text-1",
				"object":  "chat.completion",
				"created": 1,
				"model":   "chat-public",
				"choices": []map[string]any{{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": content,
					},
					"finish_reason": "stop",
				}},
				"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5},
			})
		case "/v1/messages":
			if got := r.Header.Get("x-api-key"); got != "anthropic-key" {
				t.Fatalf("anthropic x-api-key = %q, want anthropic-key", got)
			}
			if stream {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "event: message_start\n")
				_, _ = fmt.Fprint(w, "data: {\"message\":{\"id\":\"anth-stream-1\",\"model\":\"anthropic-public\",\"usage\":{\"input_tokens\":3}}}\n\n")
				_, _ = fmt.Fprint(w, "event: content_block_start\n")
				_, _ = fmt.Fprint(w, "data: {\"content_block\":{\"type\":\"text\",\"text\":\"anthropic stream\"}}\n\n")
				_, _ = fmt.Fprint(w, "event: content_block_delta\n")
				_, _ = fmt.Fprint(w, "data: {\"delta\":{\"text\":\" done\"}}\n\n")
				_, _ = fmt.Fprint(w, "event: content_block_start\n")
				_, _ = fmt.Fprint(w, "data: {\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool-1\",\"name\":\"lookup\",\"input\":{\"city\":\"Shanghai\"}}}\n\n")
				_, _ = fmt.Fprint(w, "event: content_block_stop\n")
				_, _ = fmt.Fprint(w, "data: {}\n\n")
				_, _ = fmt.Fprint(w, "event: message_delta\n")
				_, _ = fmt.Fprint(w, "data: {\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":4}}\n\n")
				_, _ = fmt.Fprint(w, "event: message_stop\n")
				_, _ = fmt.Fprint(w, "data: {}\n\n")
				return
			}
			content := []map[string]any{{"type": "text", "text": "anthropic hello"}}
			if hasTools {
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    "tool-1",
					"name":  "lookup",
					"input": map[string]any{"city": "Shanghai"},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":            "anth-msg-1",
				"type":          "message",
				"role":          "assistant",
				"content":       content,
				"model":         "anthropic-public",
				"stop_reason":   ternary(hasTools, "tool_use", "end_turn"),
				"stop_sequence": "",
				"usage":         map[string]any{"input_tokens": 3, "output_tokens": 4},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func doRequest(t *testing.T, client *http.Client, method, url string, headers map[string]string, payload any) (*http.Response, []byte) {
	t.Helper()

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal payload: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do(%s %s): %v", method, url, err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("io.ReadAll(%s %s): %v", method, url, err)
	}
	return resp, raw
}

func authHeaders(token string) map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  "application/json",
	}
}

func assertStatus(t *testing.T, resp *http.Response, want int, body []byte) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, want, body)
	}
}

func decodeJSONMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal(%q): %v", string(body), err)
	}
	return payload
}

func parseSSEData(body []byte) []string {
	lines := strings.Split(string(body), "\n")
	events := make([]string, 0)
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			events = append(events, strings.TrimSpace(strings.TrimPrefix(line, "data: ")))
		}
	}
	return events
}

func collectTypes(t *testing.T, events []string) []string {
	t.Helper()
	types := make([]string, 0, len(events))
	for _, event := range events {
		payload := decodeJSONMap(t, []byte(event))
		if value, ok := payload["type"].(string); ok {
			types = append(types, value)
		}
	}
	return types
}

func bodyJSONSnippets(events []string) []string {
	return append([]string(nil), events...)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) || value == want {
			return true
		}
	}
	return false
}

func decodeMaybeJSON(body []byte) map[string]any {
	var payload map[string]any
	_ = json.Unmarshal(body, &payload)
	return payload
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func ternary(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}
