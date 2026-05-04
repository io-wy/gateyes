package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/alert"
	"github.com/gateyes/gateway/internal/service/budget"
	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
	"github.com/gateyes/gateway/internal/service/router"
)

// TestE2EFeatures tests all 5 new features end-to-end:
// 1. Virtual Keys (LiteLLM-style mapping)
// 2. Accurate Token Counting (provider-returned usage)
// 3. Response Count / Pagination (meta.total)
// 4. Webhook/Callback (per-VK callback_url)
// 5. Redis Distributed Rate Limiting
func TestE2EFeatures(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := newFeatureUpstream(t)
	t.Cleanup(upstream.Close)

	callbackReceived := &atomic.Int32{}
	callbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		callbackReceived.Add(1)
		t.Logf("callback received: %s", string(body))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(callbackServer.Close)

	ctx := context.Background()
	database, err := db.Open(config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "features-e2e.db"),
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
		t.Fatalf("migrate: %v", err)
	}

	store := sqlstore.New(database)
	tenant, err := store.EnsureTenant(ctx, repository.EnsureTenantParams{
		ID: "feat-tenant", Slug: "feat-tenant", Name: "feat-tenant", Status: repository.StatusActive,
	})
	if err != nil {
		t.Fatalf("ensure tenant: %v", err)
	}
	tenantID := tenant.ID
	if err := store.ReplaceTenantProviders(ctx, tenantID, []string{"openai-feat"}); err != nil {
		t.Fatalf("replace providers: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID: tenantID, Key: "parent-key", SecretHash: repository.HashSecret("parent-secret"),
		Name: "parent-user", Email: "parent@test.com", Role: repository.RoleTenantAdmin, Quota: 100000, QPS: 100,
	}); err != nil {
		t.Fatalf("seed parent key: %v", err)
	}

	cfgObj := &config.Config{
		Server:  config.ServerConfig{ListenAddr: ":0", ReadTimeout: 30, WriteTimeout: 300},
		Metrics: config.MetricsConfig{Namespace: fmt.Sprintf("feat_e2e_%d", time.Now().UnixNano())},
		Router:  config.RouterConfig{Strategy: "round_robin"},
		Providers: []config.ProviderConfig{
			{Name: "openai-feat", Type: "openai", BaseURL: upstream.URL, Endpoint: "chat",
				APIKey: "upstream-key", Model: "feat-model", Timeout: 5, Enabled: true, MaxTokens: 256,
				PriceInput: 0.01, PriceOutput: 0.03},
		},
	}

	metrics := NewMetrics(cfgObj.Metrics.Namespace)
	providerMgr, _ := provider.NewManager(cfgObj.Providers)
	if records, err := store.ListProviderRegistry(ctx); err == nil {
		providerMgr.ApplyRegistry(records)
	}
	routerSvc := router.NewRouter(cfgObj.Router, nil)
	routerSvc.SetProviders(providerMgr.List())
	limiterSvc := limiter.NewLimiter(config.LimiterConfig{
		GlobalQPS: 100, GlobalTPM: 100000, GlobalTokenBurst: 100000,
		PerUserRequestBurst: 100, QueueSize: 128,
	})
	t.Cleanup(limiterSvc.Stop)
	budgetSvc := budget.New(store)
	alertSvc := alert.NewAlertService(config.AlertConfig{Enabled: false}, store)
	mw := middleware.New(store, limiterSvc, budgetSvc, alertSvc, metrics)
	responseService := responseSvc.New(&responseSvc.Dependencies{
		Config: cfgObj, Store: store, Auth: mw.AuthService(),
		ProviderMgr: providerMgr, Router: routerSvc, Alert: alertSvc, Limiter: limiterSvc,
	})
	catalogSvc := catalog.New(&catalog.Dependencies{
		Store: store, Auth: mw.AuthService(), Limiter: limiterSvc,
		BudgetSvc: budgetSvc, AlertSvc: alertSvc, Responses: responseService,
	})
	h := NewHandler(&Dependencies{
		Config: cfgObj, Store: store, Metrics: metrics,
		ProviderMgr: providerMgr, ResponseSvc: responseService, CatalogSvc: catalogSvc,
	})
	adminHandler := NewAdminHandler(store, providerMgr, catalogSvc, nil)
	srv := NewServer(cfgObj.Server, h, adminHandler, mw, nil, nil)
	ts := httptest.NewServer(srv.engine)
	t.Cleanup(ts.Close)

	parentToken := "parent-key:parent-secret"

	t.Run("Feature1_VirtualKeyLifecycle", func(t *testing.T) {
		// 1a. List API keys to get parent key ID
		resp, body := doReq(t, ts, "GET", "/admin/keys", parentToken, nil)
		assertStatus(t, resp, http.StatusOK, body)
		keys := decodeJSONMap(t, body)["data"].([]any)
		if len(keys) == 0 {
			t.Fatal("need at least one API key")
		}
		apiKeyID := keys[0].(map[string]any)["id"].(string)

		// 1b. Create virtual key with budget, rate limit, models, callback
		resp, body = doReq(t, ts, "POST", "/admin/virtual-keys", parentToken, map[string]any{
			"user_id":           "user-1",
			"api_key_id":       apiKeyID,
			"name":             "e2e-vk",
			"budget_usd":       5.0,
			"budget_policy":    "hard_reject",
			"rate_limit_qps":   100,
			"allowed_models":   []string{"feat-model"},
			"allowed_providers": []string{"openai-feat"},
			"callback_url":     callbackServer.URL,
		})
		assertStatus(t, resp, http.StatusCreated, body)
		vkData := decodeJSONMap(t, body)["data"].(map[string]any)
		vkToken := vkData["token"].(string)
		vkID := vkData["id"].(string)
		t.Logf("VK created: id=%s token=%s", vkID, vkToken)

		if vkData["secret"] == nil || vkData["secret"] == "" {
			t.Fatal("VK create should return secret")
		}

		// 1c. Get VK by ID
		resp, body = doReq(t, ts, "GET", "/admin/virtual-keys/"+vkID, parentToken, nil)
		assertStatus(t, resp, http.StatusOK, body)

		// 1d. List VKs
		resp, body = doReq(t, ts, "GET", "/admin/virtual-keys", parentToken, nil)
		assertStatus(t, resp, http.StatusOK, body)
		vkList := decodeJSONMap(t, body)["data"].([]any)
		if len(vkList) == 0 {
			t.Fatal("VK list should not be empty")
		}

		// 1e. Update VK
		newName := "e2e-vk-renamed"
		resp, body = doReq(t, ts, "PUT", "/admin/virtual-keys/"+vkID, parentToken, map[string]any{
			"name": newName,
		})
		assertStatus(t, resp, http.StatusOK, body)
		if decodeJSONMap(t, body)["data"].(map[string]any)["name"] != newName {
			t.Fatal("VK update should change name")
		}

		// 1f. Make request using VK token (authenticates through VK → parent)
		resp, body = doReq(t, ts, "POST", "/v1/chat/completions", vkToken, map[string]any{
			"model": "feat-model",
			"messages": []map[string]any{{
				"role": "user", "content": "hello via virtual key",
			}},
			"max_tokens": 64,
		})
		assertStatus(t, resp, http.StatusOK, body)
		payload := decodeJSONMap(t, body)
		choice := payload["choices"].([]any)[0].(map[string]any)
		msg := choice["message"].(map[string]any)
		if msg["content"] != "feat hello" {
			t.Fatalf("VK chat content = %v, want feat hello", msg["content"])
		}
		usage := payload["usage"].(map[string]any)
		t.Logf("VK request usage: %v", usage)

		// 1g. Verify budget was consumed
		resp, body = doReq(t, ts, "GET", "/admin/virtual-keys/"+vkID, parentToken, nil)
		assertStatus(t, resp, http.StatusOK, body)
		vkAfter := decodeJSONMap(t, body)["data"].(map[string]any)
		spent := vkAfter["spent_usd"].(float64)
		if spent <= 0 {
			t.Fatalf("VK spent_usd = %f, should be > 0 after request", spent)
		}
		t.Logf("VK budget: %.4f / %.2f USD", spent, vkAfter["budget_usd"])

		// 1h. Test VK model restriction — use disallowed model
		resp, body = doReq(t, ts, "POST", "/v1/chat/completions", vkToken, map[string]any{
			"model": "forbidden-model",
			"messages": []map[string]any{{"role": "user", "content": "should fail"}},
		})
		if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("VK with disallowed model: status=%d, want 403/400: %s", resp.StatusCode, body)
		}
		t.Logf("VK model restriction works: status=%d", resp.StatusCode)

		// 1i. Delete VK
		resp, body = doReq(t, ts, "DELETE", "/admin/virtual-keys/"+vkID, parentToken, nil)
		assertStatus(t, resp, http.StatusOK, body)

		// 1j. Verify VK deleted — token should no longer work
		resp, body = doReq(t, ts, "POST", "/v1/chat/completions", vkToken, map[string]any{
			"model": "feat-model",
			"messages": []map[string]any{{"role": "user", "content": "should fail"}},
		})
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("Deleted VK should not authenticate: status=%d: %s", resp.StatusCode, body)
		}
	})

	t.Run("Feature2_AccurateTokenCounting", func(t *testing.T) {
		// Make a request and verify provider-returned usage is stored
		resp, body := doReq(t, ts, "POST", "/v1/chat/completions", parentToken, map[string]any{
			"model": "feat-model",
			"messages": []map[string]any{{"role": "user", "content": "count tokens"}},
			"max_tokens": 64,
		})
		assertStatus(t, resp, http.StatusOK, body)
		payload := decodeJSONMap(t, body)
		usage := payload["usage"].(map[string]any)

		pt := int(usage["prompt_tokens"].(float64))
		ct := int(usage["completion_tokens"].(float64))
		tt := int(usage["total_tokens"].(float64))

		if pt != 7 || ct != 13 || tt != 20 {
			t.Fatalf("usage mismatch: prompt=%d completion=%d total=%d, want 7/13/20", pt, ct, tt)
		}
		if tt != pt+ct {
			t.Fatalf("total_tokens(%d) != prompt(%d) + completion(%d)", tt, pt, ct)
		}
		t.Logf("Token counting verified: prompt=%d completion=%d total=%d", pt, ct, tt)
	})

	t.Run("Feature3_ResponseCountAndPagination", func(t *testing.T) {
		// Make multiple requests to create responses
		for i := 0; i < 3; i++ {
			resp, body := doReq(t, ts, "POST", "/v1/chat/completions", parentToken, map[string]any{
				"model": "feat-model",
				"messages": []map[string]any{{"role": "user", "content": fmt.Sprintf("request %d", i)}},
				"max_tokens": 64,
			})
			assertStatus(t, resp, http.StatusOK, body)
		}

		// List responses and verify meta.total
		resp, body := doReq(t, ts, "GET", "/admin/responses", parentToken, nil)
		assertStatus(t, resp, http.StatusOK, body)
		result := decodeJSONMap(t, body)
		data := result["data"].([]any)
		meta := result["meta"].(map[string]any)

		total := int(meta["total"].(float64))
		if total < 3 {
			t.Fatalf("meta.total = %d, want >= 3", total)
		}
		if len(data) == 0 {
			t.Fatal("response list should not be empty")
		}
		t.Logf("Response count: returned=%d total=%d", len(data), total)

		// Verify response structure includes provider and model
		first := data[0].(map[string]any)
		if first["provider_name"] == "" || first["model"] == "" {
			t.Fatalf("response missing provider/model: %#v", first)
		}
	})

	t.Run("Feature4_CallbackOnVKRequest", func(t *testing.T) {
		// Create VK with callback URL
		resp, body := doReq(t, ts, "GET", "/admin/keys", parentToken, nil)
		assertStatus(t, resp, http.StatusOK, body)
		keys := decodeJSONMap(t, body)["data"].([]any)
		apiKeyID := keys[0].(map[string]any)["id"].(string)

		resp, body = doReq(t, ts, "POST", "/admin/virtual-keys", parentToken, map[string]any{
			"user_id":       "cb-user",
			"api_key_id":   apiKeyID,
			"name":         "callback-vk",
			"budget_usd":   100.0,
			"callback_url": callbackServer.URL,
		})
		assertStatus(t, resp, http.StatusCreated, body)
		vkToken := decodeJSONMap(t, body)["data"].(map[string]any)["token"].(string)

		// Reset counter
		callbackReceived.Store(0)

		// Make request via VK
		resp, body = doReq(t, ts, "POST", "/v1/chat/completions", vkToken, map[string]any{
			"model": "feat-model",
			"messages": []map[string]any{{"role": "user", "content": "trigger callback"}},
			"max_tokens": 64,
		})
		assertStatus(t, resp, http.StatusOK, body)

		// Wait for async callback
		deadline := time.After(3 * time.Second)
		for {
			if callbackReceived.Load() > 0 {
				break
			}
			select {
			case <-deadline:
				t.Fatal("callback not received within 3 seconds")
			default:
				time.Sleep(50 * time.Millisecond)
			}
		}
		t.Logf("Callback received: count=%d", callbackReceived.Load())
	})

	t.Run("Feature5_RedisRateLimiting", func(t *testing.T) {
		rdb := redis.NewClient(&redis.Options{
			Addr:     "127.0.0.1:6379",
			Password: "dev_redis_pw_2026",
		})
		defer rdb.Close()
		if err := rdb.Ping(ctx).Err(); err != nil {
			t.Skipf("Redis not available: %v", err)
		}

		// Create a limiter with low limits and Redis backend
		rl := limiter.NewLimiter(config.LimiterConfig{
			GlobalQPS: 10000, GlobalTPM: 100000, GlobalTokenBurst: 100000,
			PerUserRequestBurst: 100,
			TenantTPM:           600, TenantTPMBurst: 3,
			ProviderTPM:         600, ProviderTPMBurst: 3,
			QueueSize: 128,
		})
		rl.SetRedis(rdb)
		t.Cleanup(rl.Stop)

		// Clean Redis keys
		rdb.Del(ctx, "gateyes:rl:ten:redis-tenant:t", "gateyes:rl:ten:redis-tenant:r",
			"gateyes:rl:prov:redis-prov:t", "gateyes:rl:prov:redis-prov:r")

		// First 3 should pass (within burst)
		for i := 0; i < 3; i++ {
			if !rl.CheckTenant("redis-tenant", 1) {
				t.Fatalf("tenant request %d should pass within burst", i+1)
			}
		}
		// 4th should be denied
		if rl.CheckTenant("redis-tenant", 1) {
			t.Fatal("tenant should be rate limited after burst exhausted")
		}

		// Provider dimension
		for i := 0; i < 3; i++ {
			if !rl.CheckProvider("redis-prov", 1) {
				t.Fatalf("provider request %d should pass within burst", i+1)
			}
		}
		if rl.CheckProvider("redis-prov", 1) {
			t.Fatal("provider should be rate limited after burst exhausted")
		}

		// Different tenant/provider should not be affected
		if !rl.CheckTenant("other-tenant", 1) {
			t.Fatal("different tenant should not be affected")
		}
		if !rl.CheckProvider("other-prov", 1) {
			t.Fatal("different provider should not be affected")
		}

		// Verify keys exist in Redis
		keys, _ := rdb.Keys(ctx, "gateyes:rl:ten:redis-tenant:*").Result()
		if len(keys) == 0 {
			t.Fatal("expected Redis keys for rate-limited tenant")
		}
		t.Logf("Redis rate limiting verified: %d keys found", len(keys))
	})
}

func doReq(t *testing.T, ts *httptest.Server, method, path, token string, payload any) (*http.Response, []byte) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		body = strings.NewReader(string(raw))
	}
	req, err := http.NewRequest(method, ts.URL+path, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, raw
}

func newFeatureUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "feat-chat-1",
				"object":  "chat.completion",
				"created": 1,
				"model":   "feat-model",
				"choices": []map[string]any{{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "feat hello",
					},
					"finish_reason": "stop",
				}},
				"usage": map[string]any{
					"prompt_tokens":     7,
					"completion_tokens": 13,
					"total_tokens":      20,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
}
