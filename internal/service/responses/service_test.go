package responses

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gateyes/gateway/internal/service/router"
)

func TestCreateCachesAndReusesResponse(t *testing.T) {
	var upstreamCalls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalls, 1)
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-upstream",
			"object":  "chat.completion",
			"created": 1700000000,
			"model":   "provider-model",
			"choices": []map[string]any{{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "cached hello",
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

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL:  upstream.URL,
		endpoint:     "chat",
		cacheEnabled: true,
		providers:    []string{"test-openai"},
	})

	req := &provider.ResponseRequest{
		Model: "public-model",
		Input: []provider.Message{{Role: "user", Content: "hello"}},
	}

	first, err := env.service.Create(context.Background(), env.identity, req, "")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := env.service.Create(context.Background(), env.identity, req, "")
	if err != nil {
		t.Fatalf("second create: %v", err)
	}

	if atomic.LoadInt32(&upstreamCalls) != 1 {
		t.Fatalf("expected 1 upstream call, got %d", upstreamCalls)
	}
	if first.CacheHit {
		t.Fatalf("expected first request to miss cache")
	}
	if !second.CacheHit {
		t.Fatalf("expected second request to hit cache")
	}
	if got := second.Response.OutputText(); got != "cached hello" {
		t.Fatalf("unexpected cached output: %q", got)
	}
	if second.ProviderName != "cache" {
		t.Fatalf("expected cache provider, got %q", second.ProviderName)
	}

	stats, err := env.store.GetUsageSummary(context.Background(), env.identity.TenantID)
	if err != nil {
		t.Fatalf("usage summary: %v", err)
	}
	if stats.SuccessRequests != 2 {
		t.Fatalf("expected 2 successful usage records, got %d", stats.SuccessRequests)
	}
}

func TestCreateMarksUsageAndResponseOnUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusBadGateway)
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL:  upstream.URL,
		endpoint:     "chat",
		cacheEnabled: false,
		providers:    []string{"test-openai"},
	})

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if err == nil {
		t.Fatalf("expected upstream error")
	}

	stats, err := env.store.GetUsageSummary(context.Background(), env.identity.TenantID)
	if err != nil {
		t.Fatalf("usage summary: %v", err)
	}
	if stats.FailedRequests != 1 {
		t.Fatalf("expected 1 failed usage record, got %d", stats.FailedRequests)
	}

	var count int
	var status string
	if err := env.database.Conn.QueryRowContext(context.Background(), `
SELECT COUNT(1), MAX(status)
FROM responses`).Scan(&count, &status); err != nil {
		t.Fatalf("query responses: %v", err)
	}
	if count != 1 || status != "error" {
		t.Fatalf("expected one error response record, got count=%d status=%q", count, status)
	}
}

func TestCreateStreamPersistsCompletedResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"stream hello\"}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_up\",\"created_at\":1700000000,\"model\":\"provider-model\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"stream hello\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":2,\"total_tokens\":5}}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL:  upstream.URL,
		endpoint:     "responses",
		cacheEnabled: false,
		providers:    []string{"test-openai"},
	})

	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input:  "hello",
		Stream: true,
	}, "session-1")
	if err != nil {
		t.Fatalf("create stream: %v", err)
	}

	var eventTypes []string
	var streamErr error
	eventsCh := stream.Events
	errCh := stream.Errors
	for eventsCh != nil || errCh != nil {
		select {
		case event, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				continue
			}
			eventTypes = append(eventTypes, event.Type)
		case err, ok := <-errCh:
			if !ok {
				errCh = nil
				continue
			}
			if err != nil {
				streamErr = err
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for stream")
		}
	}

	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
	if len(eventTypes) < 2 || eventTypes[0] != "response.created" || eventTypes[len(eventTypes)-1] != "response.completed" {
		t.Fatalf("unexpected event sequence: %v", eventTypes)
	}

	stats, err := env.store.GetUsageSummary(context.Background(), env.identity.TenantID)
	if err != nil {
		t.Fatalf("usage summary: %v", err)
	}
	if stats.SuccessRequests != 1 {
		t.Fatalf("expected 1 successful usage record, got %d", stats.SuccessRequests)
	}

	var responseBody string
	if err := env.database.Conn.QueryRowContext(context.Background(), `
SELECT response_body
FROM responses
LIMIT 1`).Scan(&responseBody); err != nil {
		t.Fatalf("query response body: %v", err)
	}
	if !strings.Contains(responseBody, "stream hello") {
		t.Fatalf("expected persisted response body to contain stream text, got %q", responseBody)
	}
}

type responsesTestEnv struct {
	database *db.DB
	store    *sqlstore.Store
	service  *Service
	identity *repository.AuthIdentity
}

type responsesTestEnvConfig struct {
	upstreamURL  string
	endpoint     string
	cacheEnabled bool
	providers    []string
}

func newResponsesTestEnv(t *testing.T, cfg responsesTestEnvConfig) *responsesTestEnv {
	t.Helper()

	ctx := context.Background()
	database, err := db.Open(config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "responses.db"),
		AutoMigrate:            true,
		MaxOpenConns:           1,
		MaxIdleConns:           1,
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
	if err := store.ReplaceTenantProviders(ctx, tenant.ID, cfg.providers); err != nil {
		t.Fatalf("replace tenant providers: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenant.ID,
		Key:        "test-key",
		SecretHash: repository.HashSecret("test-secret"),
		Name:       "test-user",
		Email:      "test@example.com",
		Role:       repository.RoleTenantUser,
		Quota:      1000,
		QPS:        100,
		Models:     nil,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	authSvc := auth.NewAuth(store)
	identity, err := authSvc.Authenticate(ctx, "test-key", "test-secret")
	if err != nil {
		t.Fatalf("authenticate identity: %v", err)
	}

	providerMgr, err := provider.NewManager([]config.ProviderConfig{{
		Name:      "test-openai",
		Type:      "openai",
		BaseURL:   cfg.upstreamURL,
		Endpoint:  cfg.endpoint,
		APIKey:    "upstream-key",
		Model:     "provider-model",
		Timeout:   5,
		Enabled:   true,
		MaxTokens: 256,
	}})
	if err != nil {
		t.Fatalf("new provider manager: %v", err)
	}

	routerSvc := router.NewRouter(config.RouterConfig{Strategy: "round_robin"})
	routerSvc.SetProviders(providerMgr.List())

	var cacheSvc *cache.Cache
	if cfg.cacheEnabled {
		cacheSvc = cache.NewMemoryCache(config.CacheConfig{
			Enabled: true,
			MaxSize: 32,
			TTL:     60,
		})
	}

	service := New(&Dependencies{
		Config: &config.Config{
			Cache: config.CacheConfig{
				Enabled: cfg.cacheEnabled,
				MaxSize: 32,
				TTL:     60,
			},
		},
		Store:       store,
		Auth:        authSvc,
		ProviderMgr: providerMgr,
		Router:      routerSvc,
		Cache:       cacheSvc,
		Alert:       nil,
	})

	return &responsesTestEnv{
		database: database,
		store:    store,
		service:  service,
		identity: identity,
	}
}

func queryResponseStatus(ctx context.Context, conn *sql.DB) (int, string, error) {
	var count int
	var status string
	err := conn.QueryRowContext(ctx, `
SELECT COUNT(1), MAX(status)
FROM responses`).Scan(&count, &status)
	return count, status, err
}
