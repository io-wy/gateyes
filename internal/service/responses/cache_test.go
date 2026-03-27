package responses

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/cache"
	"github.com/gateyes/gateway/internal/service/provider"
	"github.com/gateyes/gateway/internal/service/router"
)

// mockStoreWithCreateError returns error on CreateResponse
type mockStoreWithCreateError struct {
	repository.Store
	createResponseErr error
}

func (m *mockStoreWithCreateError) CreateResponse(ctx context.Context, record repository.ResponseRecord) error {
	return m.createResponseErr
}

func TestInvalidCacheSkipsAndContinuesToUpstream(t *testing.T) {
	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		_, _ = w.Write([]byte(`{"id":"chatcmpl-upstream","object":"chat.completion","created":1700000000,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`))
	}))
	defer upstream.Close()

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
		BaseURL:   upstream.URL,
		Endpoint:  "chat",
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

	cacheSvc := cache.NewMemoryCache(config.CacheConfig{
		Enabled: true,
		MaxSize: 32,
		TTL:     60,
	})
	cacheSvc.Set("test-prompt", "invalid-json-data")

	service := New(&Dependencies{
		Config: &config.Config{
			Cache: config.CacheConfig{
				Enabled: true,
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

	result, err := service.Create(context.Background(), identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input: "test-prompt",
	}, "")

	if !upstreamCalled {
		t.Fatal("Expected upstream to be called when cache has invalid data")
	}
	if err != nil {
		t.Fatalf("Expected no error when invalid cache is skipped, got: %v", err)
	}
	if result == nil || result.Response == nil {
		t.Fatal("Expected result when invalid cache is skipped")
	}
}

func TestCacheHitCreateResponseFailureReturnsError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"chatcmpl-upstream","object":"chat.completion","created":1700000000,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`))
	}))
	defer upstream.Close()

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
		BaseURL:   upstream.URL,
		Endpoint:  "chat",
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

	cacheSvc := cache.NewMemoryCache(config.CacheConfig{
		Enabled: true,
		MaxSize: 32,
		TTL:     60,
	})

	service := New(&Dependencies{
		Config: &config.Config{
			Cache: config.CacheConfig{
				Enabled: true,
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

	// First request to populate cache
	_, err = service.Create(context.Background(), identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input:  "cache-test-prompt",
	}, "")
	if err != nil {
		t.Fatalf("First request to populate cache failed: %v", err)
	}

	// Create mock store that returns error
	mockStore := &mockStoreWithCreateError{
		Store:            store,
		createResponseErr: errors.New("database error: connection failed"),
	}

	serviceWithMockStore := New(&Dependencies{
		Config: &config.Config{
			Cache: config.CacheConfig{
				Enabled: true,
				MaxSize: 32,
				TTL:     60,
			},
		},
		Store:       mockStore,
		Auth:        authSvc,
		ProviderMgr: providerMgr,
		Router:      routerSvc,
		Cache:       cacheSvc,
		Alert:       nil,
	})

	// Hit cache, but CreateResponse fails
	_, err = serviceWithMockStore.Create(context.Background(), identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input:  "cache-test-prompt",
	}, "")

	if err == nil {
		t.Fatal("Expected error when CreateResponse fails on cache hit")
	}
	if !containsString(err.Error(), "failed to create cache response record") {
		t.Fatalf("Expected error message about cache response record, got: %v", err)
	}
}

func TestIsValidCacheResponseString(t *testing.T) {
	env := newTestEnvWithDefaults(t)

	// Test with valid JSON
	validJSON := `{"id":"test-id","object":"chat.completion","created":1700000000,"model":"test-model","output":[{"type":"message","role":"assistant","content":[{"type":"text","text":"hello"}]}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`
	if !env.service.isValidCacheResponseString(validJSON) {
		t.Error("Expected valid JSON to pass validation")
	}

	// Test with invalid JSON
	invalidJSON := `not-json-at-all`
	if env.service.isValidCacheResponseString(invalidJSON) {
		t.Error("Expected invalid JSON to fail validation")
	}

	// Test with empty string
	if env.service.isValidCacheResponseString("") {
		t.Error("Expected empty string to fail validation")
	}

	// Test with empty object
	if env.service.isValidCacheResponseString("{}") {
		t.Error("Expected empty object to fail validation")
	}

	// Test with missing required fields
	missingFields := `{"id":"test-id"}`
	if env.service.isValidCacheResponseString(missingFields) {
		t.Error("Expected missing fields to fail validation")
	}
}

func TestCircuitBreakerGetAllStates(t *testing.T) {
	cb := NewCircuitBreaker(config.CircuitBreakerConfig{
		FailureThreshold:  2,
	})

	// Initially empty
	states := cb.GetAllStates()
	if len(states) != 0 {
		t.Errorf("Expected empty states initially, got %d", len(states))
	}

	// Record failures to trigger circuit open
	cb.RecordFailure("tenant-a", "provider-a")
	cb.RecordFailure("tenant-a", "provider-a")

	// Should have one state now
	states = cb.GetAllStates()
	if len(states) != 1 {
		t.Errorf("Expected 1 state, got %d", len(states))
	}

	// Test with a new tenant/provider
	states2 := cb.GetAllStates()
	if len(states2) != 1 {
		t.Errorf("Expected still 1 state, got %d", len(states2))
	}
}

func TestSelectProvider(t *testing.T) {
	env := newTestEnvWithDefaults(t)

	// Get candidates
	candidates := env.service.getCandidateProviders(context.Background(), env.identity, "", "public-model")
	if len(candidates) == 0 {
		t.Fatal("Expected at least one provider")
	}

	// Test selectProvider with no error
	selected, err := env.service.selectProvider(context.Background(), env.identity, "", "public-model")

	if err != nil {
		t.Fatalf("selectProvider returned error: %v", err)
	}
	if selected == nil {
		t.Error("Expected a provider to be selected")
	}
}

func TestGetCandidateProviders(t *testing.T) {
	env := newTestEnvWithDefaults(t)

	// Test with empty session
	candidates := env.service.getCandidateProviders(context.Background(), env.identity, "", "public-model")
	if len(candidates) == 0 {
		t.Error("Expected at least one candidate provider")
	}

	// Test with session
	candidatesWithSession := env.service.getCandidateProviders(context.Background(), env.identity, "session-123", "public-model")
	if len(candidatesWithSession) == 0 {
		t.Error("Expected at least one candidate with session")
	}
}

func TestIsStreamRetryable(t *testing.T) {
	env := newTestEnvWithDefaults(t)

	// Test isStreamRetryable with nil error - it returns true!
	if !env.service.isStreamRetryable(nil) {
		t.Error("Expected nil error to be retryable (returns true)")
	}

	// Test with unauthorized error - should not be retryable
	if env.service.isStreamRetryable(ErrUnauthorized) {
		t.Error("Expected ErrUnauthorized to not be retryable")
	}

	// Test with forbidden error - should not be retryable
	if env.service.isStreamRetryable(ErrForbidden) {
		t.Error("Expected ErrForbidden to not be retryable")
	}
}

func TestStream(t *testing.T) {
	env := newTestEnvWithDefaults(t)

	// Test stream creation
	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input:  "test",
		Stream: true,
	}, "")

	if err != nil {
		t.Fatalf("CreateStream failed: %v", err)
	}
	if stream == nil {
		t.Fatal("Expected stream to be created")
	}

	// Let the stream run for a bit
	select {
	case <-stream.Events:
		// Got an event
	case <-stream.Errors:
		// Got an error
	case <-context.Background().Done():
	}
}

func TestGetCircuitBreakerStates(t *testing.T) {
	env := newTestEnvWithDefaults(t)

	// Record some failures to have states
	env.service.circuitBreaker.RecordFailure("tenant-a", "provider-a")
	env.service.circuitBreaker.RecordFailure("tenant-a", "provider-a")

	// Get states
	states := env.service.GetCircuitBreakerStates()
	if len(states) == 0 {
		t.Error("Expected some circuit breaker states")
	}
}

func TestNormalizeResponse(t *testing.T) {
	env := newTestEnvWithDefaults(t)

	exec := &execution{
		requestedModel: "test-model",
		responseID:     "test-id",
		tenantID:       "tenant-a",
		provider:       nil, // nil provider
	}

	// Test normalizeResponse with nil provider - should not panic
	resp := &provider.Response{
		ID:     "test-id",
		Model:  "test-model",
		Output: []provider.ResponseOutput{},
	}

	result := env.service.normalizeResponse(exec, resp)
	if result == nil {
		t.Error("Expected normalizeResponse to return a response")
	}
}

func TestBuildUpstreamRequest(t *testing.T) {
	req := &provider.ResponseRequest{
		Model:           "test-model",
		Input:           "test input",
		Messages:        []provider.Message{{Role: "user", Content: "test"}},
		Stream:          false,
		MaxOutputTokens: 100,
		MaxTokens:       200,
	}

	upstreamReq := buildUpstreamRequest(req)

	if upstreamReq.Model != req.Model {
		t.Error("Expected model to match")
	}
	if upstreamReq.Stream != req.Stream {
		t.Error("Expected stream to match")
	}
	if upstreamReq.MaxOutputTokens != req.MaxOutputTokens {
		t.Error("Expected MaxOutputTokens to match")
	}
}

// Helper functions

func containsString(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && len(s) >= len(substr) && (s == substr || containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func newTestEnvWithDefaults(t *testing.T) *responsesTestEnv {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"chatcmpl-upstream","object":"chat.completion","created":1700000000,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`))
	}))
	t.Cleanup(upstream.Close)

	return newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL:  upstream.URL,
		endpoint:     "chat",
		cacheEnabled: false,
		providers:    []string{"test-openai"},
	})
}
