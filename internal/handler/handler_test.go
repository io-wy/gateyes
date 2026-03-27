package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
	"github.com/gateyes/gateway/internal/service/router"
)

func TestResponsesEndpointReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if len(payload.Output) == 0 || len(payload.Output[0].Content) == 0 || payload.Output[0].Content[0].Text != "handler hello" {
		t.Fatalf("unexpected output payload: %s", rec.Body.String())
	}
}

func TestChatEndpointReturnsCompatibilityJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestResponsesEndpointStreamReturnsSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"stream handler\"}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_up\",\"created_at\":1700000000,\"model\":\"provider-model\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"stream handler\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":2,\"total_tokens\":5}}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	env := newHandlerTestEnv(t, handlerTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "responses",
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"public-model","input":"hello","stream":true}`))
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	req.Header.Set("Content-Type", "application/json")
	env.server.engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "response.created") {
		t.Fatalf("expected response.created event, got %q", body)
	}
	if !strings.Contains(body, "stream handler") {
		t.Fatalf("expected stream delta in body, got %q", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected done marker, got %q", body)
	}
}

type handlerTestEnv struct {
	server      *Server
	store       *sqlstore.Store
	providerMgr *provider.Manager
}

type handlerTestEnvConfig struct {
	upstreamURL string
	endpoint    string
}

func newHandlerTestEnv(t *testing.T, cfg handlerTestEnvConfig) *handlerTestEnv {
	t.Helper()

	ctx := context.Background()
	database, err := db.Open(config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "handler.db"),
		AutoMigrate:            true,
		MaxOpenConns:           4,
		MaxIdleConns:           4,
		ConnMaxLifetimeSeconds: 60,
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
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
	if err := store.ReplaceTenantProviders(ctx, tenant.ID, []string{"test-openai"}); err != nil {
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
			Enabled:   true,
		},
		Cache: config.CacheConfig{
			Enabled: false,
			MaxSize: 32,
			TTL:     60,
		},
		Router: config.RouterConfig{
			Strategy: "round_robin",
		},
		Providers: []config.ProviderConfig{{
			Name:      "test-openai",
			Type:      "openai",
			BaseURL:   cfg.upstreamURL,
			Endpoint:  cfg.endpoint,
			APIKey:    "upstream-key",
			Model:     "provider-model",
			Timeout:   5,
			Enabled:   true,
			MaxTokens: 256,
		}},
	}

	metrics := NewMetrics(cfgObj.Metrics.Namespace)
	providerMgr, err := provider.NewManager(cfgObj.Providers)
	if err != nil {
		t.Fatalf("new provider manager: %v", err)
	}
	routerSvc := router.NewRouter(cfgObj.Router)
	routerSvc.SetProviders(providerMgr.List())
	var cacheSvc *cache.Cache
	if cfgObj.Cache.Enabled {
		cacheSvc = cache.NewMemoryCache(cfgObj.Cache)
	}
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
		Cache:       cacheSvc,
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

	return &handlerTestEnv{
		server:      NewServer(cfgObj.Server, h, adminHandler, mw),
		store:       store,
		providerMgr: providerMgr,
	}
}
