package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/testutil"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
	"github.com/gateyes/gateway/internal/service/router"
)

func TestResponsesEndpointReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-upstream",
			"object":  "chat.completion",
			"created": 1700000000,
			"model":   "provider-model",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "handler hello",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     3,
				"completion_tokens": 2,
				"total_tokens":      5,
			},
		})
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"public-model","input":"hello"}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Object string `json:"object"`
		Model  string `json:"model"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Model != "public-model" {
		t.Fatalf("expected public model in response, got %q", payload.Model)
	}
	if payload.Object != "response" {
		t.Fatalf("expected response object, got %q", payload.Object)
	}
	if len(payload.Output) == 0 || len(payload.Output[0].Content) == 0 || payload.Output[0].Content[0].Text != "handler hello" {
		t.Fatalf("unexpected output payload: %s", rec.Body.String())
	}
}

func TestModelsEndpointSupportsCapabilityFiltersAndKeyScope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
	})
	env.providerMgr.ApplyRegistry([]repository.ProviderRegistryRecord{{
		Name:                     "test-openai",
		Enabled:                  true,
		Drain:                    false,
		HealthStatus:             provider.ProviderHealthHealthy,
		RoutingWeight:            3,
		SupportsChat:             true,
		SupportsResponses:        true,
		SupportsMessages:         false,
		SupportsStream:           true,
		SupportsTools:            true,
		SupportsImages:           true,
		SupportsStructuredOutput: true,
		SupportsLongContext:      false,
	}})

	keyRecord, err := env.store.CreateAPIKey(context.Background(), repository.CreateAPIKeyParams{
		UserID:           "test-user-id",
		Key:              "models-key",
		SecretHash:       repository.HashSecret("models-secret"),
		Status:           repository.StatusActive,
		AllowedModels:    []string{"provider-model"},
		AllowedProviders: []string{"test-openai"},
	})
	if err != nil {
		// fallback to using the seeded bootstrap user when tests run against a generated UUID user id
		seededIdentity, authErr := env.store.Authenticate(context.Background(), "test-key")
		if authErr != nil {
			t.Fatalf("Authenticate(test-key) error: %v", authErr)
		}
		keyRecord, err = env.store.CreateAPIKey(context.Background(), repository.CreateAPIKeyParams{
			UserID:           seededIdentity.UserID,
			Key:              "models-key",
			SecretHash:       repository.HashSecret("models-secret"),
			Status:           repository.StatusActive,
			AllowedModels:    []string{"provider-model"},
			AllowedProviders: []string{"test-openai"},
		})
		if err != nil {
			t.Fatalf("CreateAPIKey() error: %v", err)
		}
	}
	_ = keyRecord

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models?surface=responses&stream=true", nil)
	req.Header.Set("Authorization", "Bearer models-key:models-secret")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/models filtered status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string][]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(%q) error: %v", rec.Body.String(), err)
	}
	if len(payload["data"]) != 1 {
		t.Fatalf("GET /v1/models data = %#v, want one filtered model", payload["data"])
	}
	item := payload["data"][0]
	if item["provider"] != "test-openai" || item["health_status"] != provider.ProviderHealthHealthy {
		t.Fatalf("GET /v1/models item = %#v, want provider metadata", item)
	}
	caps := item["capabilities"].(map[string]any)
	if caps["responses"] != true || caps["stream"] != true {
		t.Fatalf("GET /v1/models capabilities = %#v, want responses+stream true", caps)
	}
}

func TestServiceRuntimeEndpointsWorkThroughCatalog(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chat-service",
			"object":  "chat.completion",
			"created": 1,
			"model":   "provider-model",
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": "service hello",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     2,
				"completion_tokens": 1,
				"total_tokens":      3,
			},
		})
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
	})

	createResult, err := env.catalogSvc.CreateService(context.Background(), repository.CreateServiceParams{
		TenantID:        "tenant-a",
		Name:            "Greeting Service",
		RequestPrefix:   "greeting",
		DefaultProvider: "test-openai",
		DefaultModel:    "provider-model",
		Enabled:         true,
		Config: repository.ServiceConfig{
			Surfaces: []string{"invoke", "chat", "responses"},
			PromptTemplate: &repository.PromptTemplateConfig{
				UserTemplate: "Say hello to {{name}}",
				Variables: []repository.PromptTemplateVariable{{
					Name:     "name",
					Required: true,
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error: %v", err)
	}
	if _, _, err := env.catalogSvc.PublishServiceVersion(context.Background(), "tenant-a", createResult.Service.ID, createResult.InitialVersion.ID, "published"); err != nil {
		t.Fatalf("PublishServiceVersion() error: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/service/greeting/invoke", bytes.NewBufferString(`{"variables":{"name":"Gateyes"}}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /service/greeting/invoke status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"object":"response"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`service hello`)) {
		t.Fatalf("POST /service/greeting/invoke body = %s, want response payload", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/service/greeting/chat/completions", bytes.NewBufferString(`{"messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /service/greeting/chat/completions status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"object":"chat.completion"`)) {
		t.Fatalf("POST /service/greeting/chat/completions body = %s, want chat completion payload", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/service/greeting/invoke", bytes.NewBufferString(`{"variables":{}}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /service/greeting/invoke(missing var) status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestChatEndpointReturnsCompatibilityJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-upstream",
			"object":  "chat.completion",
			"created": 1700000000,
			"model":   "provider-model",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "compat hello",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens":     3,
				"completion_tokens": 2,
				"total_tokens":      5,
			},
		})
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Object != "chat.completion" {
		t.Fatalf("expected chat.completion object, got %q", payload.Object)
	}
	if len(payload.Choices) != 1 || payload.Choices[0].Message.Content != "compat hello" {
		t.Fatalf("unexpected chat payload: %s", rec.Body.String())
	}
}

type handlerTestEnv struct {
	server           *Server
	store            *sqlstore.Store
	providerMgr      *provider.Manager
	catalogSvc       *catalog.Service
	metricsNamespace string
}

type handlerTestEnvConfig struct {
	upstreamURL     string
	endpoint        string
	tenantProviders []string
	providerConfigs []config.ProviderConfig
}

func newHandlerTestEnv(t *testing.T, cfg handlerTestEnvConfig) *handlerTestEnv {
	t.Helper()

	ctx := context.Background()
	database := testutil.OpenTestDB(t)

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
	providerCfgs := cfg.providerConfigs
	if len(providerCfgs) == 0 {
		providerCfgs = []config.ProviderConfig{{
			Name:      "test-openai",
			Type:      "openai",
			BaseURL:   cfg.upstreamURL,
			Endpoint:  cfg.endpoint,
			APIKey:    "upstream-key",
			Model:     "provider-model",
			Timeout:   5,
			Enabled:   true,
			MaxTokens: 256,
		}}
	}
	tenantProviders := cfg.tenantProviders
	if len(tenantProviders) == 0 {
		tenantProviders = make([]string, 0, len(providerCfgs))
		for _, item := range providerCfgs {
			tenantProviders = append(tenantProviders, item.Name)
		}
	}

	if err := store.ReplaceTenantProviders(ctx, tenant.ID, tenantProviders); err != nil {
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
		Models:     nil,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	cfgObj := &config.Config{
		Server: config.ServerConfig{
			ListenAddr:   ":0",
			ReadTimeout:  30,
			WriteTimeout: 300,
		},
		Metrics: config.MetricsConfig{
			Namespace: fmt.Sprintf("handler_test_%d", time.Now().UnixNano()),
		},
		Router: config.RouterConfig{
			Strategy: "round_robin",
		},
		Retry: config.RetryConfig{
			MaxRetries:     2,
			InitialDelayMs: 1,
			MaxDelayMs:     1,
			BackoffFactor:  1,
		},
		Providers: providerCfgs,
	}

	metrics := NewMetrics(cfgObj.Metrics.Namespace)
	providerMgr, err := provider.NewManager(cfgObj.Providers)
	if err != nil {
		t.Fatalf("new provider manager: %v", err)
	}
	for _, item := range cfgObj.Providers {
		if err := store.UpsertProviderRegistry(ctx, provider.DefaultRegistryRecordFromConfig(item)); err != nil {
			t.Fatalf("upsert provider registry: %v", err)
		}
	}
	if records, err := store.ListProviderRegistry(ctx); err != nil {
		t.Fatalf("list provider registry: %v", err)
	} else {
		providerMgr.ApplyRegistry(records)
	}
	routerSvc := router.NewRouter(cfgObj.Router, nil)
	routerSvc.SetProviders(providerMgr.List())
	limiterSvc := limiter.NewLimiter(config.LimiterConfig{
		GlobalQPS:           100,
		GlobalTPM:           100000,
		GlobalTokenBurst:    100000,
		PerUserRequestBurst: 100,
		QueueSize:           128,
	})
	t.Cleanup(limiterSvc.Stop)
	budgetSvc := budget.New(store)
	mw := middleware.New(nil, store, limiterSvc, budgetSvc, nil, metrics)
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
	adminHandler := NewAdminHandler(store, providerMgr, catalogSvc, nil)
	healthChecker := provider.NewHealthChecker(config.HealthCheckConfig{
		Enabled:          true,
		TimeoutSeconds:   3,
		FailureThreshold: 1,
	}, store, providerMgr, nil)
	adminHandler.SetHealthChecker(healthChecker)

	return &handlerTestEnv{
		server:           NewServer(cfgObj.Server, h, adminHandler, mw),
		store:            store,
		providerMgr:      providerMgr,
		catalogSvc:       catalogSvc,
		metricsNamespace: cfgObj.Metrics.Namespace,
	}
}
