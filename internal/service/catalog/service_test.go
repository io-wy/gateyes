package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/repository/sqlstore"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/limiter"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
	"github.com/gateyes/gateway/internal/service/router"
)

type catalogTestEnv struct {
	store    *sqlstore.Store
	auth     *auth.Auth
	service  *Service
	identity *repository.AuthIdentity
}

func newCatalogTestEnv(t *testing.T, providers []config.ProviderConfig) *catalogTestEnv {
	t.Helper()

	ctx := context.Background()
	database, err := db.Open(config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "catalog.db"),
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
	providerNames := make([]string, 0, len(providers))
	for _, item := range providers {
		providerNames = append(providerNames, item.Name)
	}
	if err := store.ReplaceTenantProviders(ctx, tenant.ID, providerNames); err != nil {
		t.Fatalf("replace tenant providers: %v", err)
	}
	if err := store.EnsureBootstrapKey(ctx, repository.BootstrapAPIKeyParams{
		TenantID:   tenant.ID,
		Key:        "test-key",
		SecretHash: repository.HashSecret("test-secret"),
		Name:       "test-user",
		Email:      "test@example.com",
		Role:       repository.RoleTenantAdmin,
		Quota:      100000,
		QPS:        100,
	}); err != nil {
		t.Fatalf("seed api key: %v", err)
	}

	authSvc := auth.NewAuth(store)
	identity, err := authSvc.Authenticate(ctx, "test-key", "test-secret")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	providerMgr, err := provider.NewManager(providers)
	if err != nil {
		t.Fatalf("new provider manager: %v", err)
	}
	for _, item := range providers {
		if err := store.UpsertProviderRegistry(ctx, provider.DefaultRegistryRecordFromConfig(item)); err != nil {
			t.Fatalf("seed provider registry: %v", err)
		}
	}
	if records, err := store.ListProviderRegistry(ctx); err != nil {
		t.Fatalf("list provider registry: %v", err)
	} else {
		providerMgr.ApplyRegistry(records)
	}

	routerSvc := router.NewRouter(config.RouterConfig{Strategy: "round_robin"})
	routerSvc.SetProviders(providerMgr.List())
	limiterSvc := limiter.NewLimiter(config.LimiterConfig{
		GlobalQPS:           100,
		GlobalTPM:           100000,
		GlobalTokenBurst:    100000,
		PerUserRequestBurst: 100,
		QueueSize:           128,
	})
	t.Cleanup(limiterSvc.Stop)

	responsesService := responseSvc.New(&responseSvc.Dependencies{
		Config:      &config.Config{Retry: config.RetryConfig{MaxRetries: 1, InitialDelayMs: 1, MaxDelayMs: 1, BackoffFactor: 1}},
		Store:       store,
		Auth:        authSvc,
		ProviderMgr: providerMgr,
		Router:      routerSvc,
		Alert:       nil,
	})

	return &catalogTestEnv{
		store:    store,
		auth:     authSvc,
		service:  New(&Dependencies{Store: store, Auth: authSvc, Limiter: limiterSvc, Responses: responsesService}),
		identity: identity,
	}
}

func TestCreatePublishAndInvokePromptUsesPublishedSnapshotAndPreferredProvider(t *testing.T) {
	alpha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chat-alpha",
			"object":  "chat.completion",
			"created": 1,
			"model":   "public-model",
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "alpha"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
		})
	}))
	defer alpha.Close()
	beta := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		messages, _ := json.Marshal(body["messages"])
		if !strings.Contains(string(messages), "Say hello to Gateyes") {
			t.Fatalf("beta upstream messages = %s, want rendered prompt", string(messages))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chat-beta",
			"object":  "chat.completion",
			"created": 1,
			"model":   "public-model",
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "beta secret"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 2, "total_tokens": 4},
		})
	}))
	defer beta.Close()

	env := newCatalogTestEnv(t, []config.ProviderConfig{
		{Name: "openai-alpha", Type: "openai", BaseURL: alpha.URL, Endpoint: "chat", APIKey: "k1", Model: "public-model", Timeout: 5, Enabled: true, MaxTokens: 256},
		{Name: "openai-beta", Type: "openai", BaseURL: beta.URL, Endpoint: "chat", APIKey: "k2", Model: "public-model", Timeout: 5, Enabled: true, MaxTokens: 256},
	})

	createResult, err := env.service.CreateService(context.Background(), repository.CreateServiceParams{
		TenantID:        env.identity.TenantID,
		Name:            "Hello Service",
		RequestPrefix:   "hello-service",
		DefaultProvider: "openai-beta",
		DefaultModel:    "public-model",
		Enabled:         true,
		Config: repository.ServiceConfig{
			Surfaces: []string{"invoke", "responses"},
			PromptTemplate: &repository.PromptTemplateConfig{
				UserTemplate: "Say hello to {{name}}",
				Variables: []repository.PromptTemplateVariable{{
					Name:     "name",
					Required: true,
				}},
			},
			Policy: &repository.ServicePolicyConfig{
				Enabled: true,
				Response: &repository.GuardrailRuleSet{
					RedactTerms: []string{"secret"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error: %v", err)
	}
	if _, _, err := env.service.PublishServiceVersion(context.Background(), env.identity.TenantID, createResult.Service.ID, createResult.InitialVersion.ID, "published"); err != nil {
		t.Fatalf("PublishServiceVersion() error: %v", err)
	}

	result, serviceRecord, err := env.service.CreatePromptInvocation(context.Background(), env.identity, "hello-service", PromptInvokeRequest{
		Variables: map[string]any{"name": "Gateyes"},
	}, "session-1")
	if err != nil {
		t.Fatalf("CreatePromptInvocation() error: %v", err)
	}
	if serviceRecord.RequestPrefix != "hello-service" {
		t.Fatalf("service request_prefix = %q, want %q", serviceRecord.RequestPrefix, "hello-service")
	}
	if result.ProviderName != "openai-beta" {
		t.Fatalf("result.ProviderName = %q, want %q", result.ProviderName, "openai-beta")
	}
	if got := result.Response.OutputText(); got != "beta [REDACTED]" {
		t.Fatalf("result.Response.OutputText() = %q, want %q", got, "beta [REDACTED]")
	}
}

func TestPromptInvocationAndResponsesPoliciesBlockInvalidContent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chat-policy",
			"object":  "chat.completion",
			"created": 1,
			"model":   "public-model",
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "blocked-output"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
		})
	}))
	defer upstream.Close()

	env := newCatalogTestEnv(t, []config.ProviderConfig{
		{Name: "openai-main", Type: "openai", BaseURL: upstream.URL, Endpoint: "chat", APIKey: "k1", Model: "public-model", Timeout: 5, Enabled: true, MaxTokens: 256},
	})

	createResult, err := env.service.CreateService(context.Background(), repository.CreateServiceParams{
		TenantID:        env.identity.TenantID,
		Name:            "Policy Service",
		RequestPrefix:   "policy-service",
		DefaultProvider: "openai-main",
		DefaultModel:    "public-model",
		Enabled:         true,
		Config: repository.ServiceConfig{
			Surfaces: []string{"invoke", "responses"},
			PromptTemplate: &repository.PromptTemplateConfig{
				UserTemplate: "Prompt {{text}}",
				Variables:    []repository.PromptTemplateVariable{{Name: "text", Required: true}},
			},
			Policy: &repository.ServicePolicyConfig{
				Enabled: true,
				Request: &repository.GuardrailRuleSet{
					BlockTerms: []string{"forbidden"},
				},
				Response: &repository.GuardrailRuleSet{
					BlockTerms: []string{"blocked-output"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error: %v", err)
	}
	if _, _, err := env.service.PublishServiceVersion(context.Background(), env.identity.TenantID, createResult.Service.ID, createResult.InitialVersion.ID, "published"); err != nil {
		t.Fatalf("PublishServiceVersion() error: %v", err)
	}

	if _, _, err := env.service.CreatePromptInvocation(context.Background(), env.identity, "policy-service", PromptInvokeRequest{
		Variables: map[string]any{"text": "forbidden"},
	}, "session-1"); !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("CreatePromptInvocation(request policy) error = %v, want %v", err, ErrPolicyViolation)
	}

	if _, _, err := env.service.Create(context.Background(), env.identity, "policy-service", "responses", &provider.ResponseRequest{
		Model:    "public-model",
		Messages: []provider.Message{{Role: "user", Content: provider.TextBlocks("hello")}},
	}, "session-2"); !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("Create(response policy) error = %v, want %v", err, ErrPolicyViolation)
	}
}

func TestInheritedPoliciesMergeAcrossTenantProjectAndService(t *testing.T) {
	var capturedMessages string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		rawMessages, _ := json.Marshal(body["messages"])
		capturedMessages = string(rawMessages)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chat-inherited",
			"object":  "chat.completion",
			"created": 1,
			"model":   "public-model",
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": "to po so",
				},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5},
		})
	}))
	defer upstream.Close()

	env := newCatalogTestEnv(t, []config.ProviderConfig{
		{Name: "openai-main", Type: "openai", BaseURL: upstream.URL, Endpoint: "chat", APIKey: "k1", Model: "public-model", Timeout: 5, Enabled: true, MaxTokens: 256},
	})

	project, err := env.store.CreateProject(context.Background(), repository.CreateProjectParams{
		TenantID: env.identity.TenantID,
		Slug:     "proj-policy",
		Name:     "Policy Project",
		Status:   repository.StatusActive,
		Policy: &repository.ServicePolicyConfig{
			Enabled: true,
			Request: &repository.GuardrailRuleSet{
				RedactTerms:   []string{"p1"},
				AllowModels:   []string{"public-model"},
				MaxInputChars: 12,
			},
			Response: &repository.GuardrailRuleSet{
				RedactTerms: []string{"po"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateProject(policy) error: %v", err)
	}

	if _, err := env.store.UpdateTenant(context.Background(), env.identity.TenantID, repository.UpdateTenantParams{
		Policy: &repository.ServicePolicyConfig{
			Enabled: true,
			Request: &repository.GuardrailRuleSet{
				RedactTerms:   []string{"t1"},
				AllowModels:   []string{"public-model", "backup-model"},
				MaxInputChars: 20,
			},
			Response: &repository.GuardrailRuleSet{
				RedactTerms: []string{"to"},
			},
		},
	}); err != nil {
		t.Fatalf("UpdateTenant(policy) error: %v", err)
	}

	createResult, err := env.service.CreateService(context.Background(), repository.CreateServiceParams{
		TenantID:        env.identity.TenantID,
		ProjectID:       project.ID,
		Name:            "Inherited Policy Service",
		RequestPrefix:   "inherit-policy",
		DefaultProvider: "openai-main",
		DefaultModel:    "public-model",
		Enabled:         true,
		Config: repository.ServiceConfig{
			Surfaces: []string{"responses"},
			Policy: &repository.ServicePolicyConfig{
				Enabled: true,
				Request: &repository.GuardrailRuleSet{
					RedactTerms:   []string{"s1"},
					MaxInputChars: 16,
				},
				Response: &repository.GuardrailRuleSet{
					RedactTerms: []string{"so"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error: %v", err)
	}
	if _, _, err := env.service.PublishServiceVersion(context.Background(), env.identity.TenantID, createResult.Service.ID, createResult.InitialVersion.ID, "published"); err != nil {
		t.Fatalf("PublishServiceVersion() error: %v", err)
	}

	result, _, err := env.service.Create(context.Background(), env.identity, "inherit-policy", "responses", &provider.ResponseRequest{
		Model: "public-model",
		Messages: []provider.Message{{
			Role:    "user",
			Content: provider.TextBlocks("t1 p1 s1"),
		}},
	}, "session-inherited")
	if err != nil {
		t.Fatalf("Create(inherited policy) error: %v", err)
	}
	if !strings.Contains(capturedMessages, "[REDACTED]") {
		t.Fatalf("upstream request body = %s, want inherited redact terms applied", capturedMessages)
	}
	if got := result.Response.OutputText(); got != "[REDACTED] [REDACTED] [REDACTED]" {
		t.Fatalf("result.Response.OutputText() = %q, want all inherited response terms redacted", got)
	}

	if _, _, err := env.service.Create(context.Background(), env.identity, "inherit-policy", "responses", &provider.ResponseRequest{
		Model: "public-model",
		Messages: []provider.Message{{
			Role:    "user",
			Content: provider.TextBlocks("1234567890123"),
		}},
	}, "session-too-long"); !errors.Is(err, ErrPolicyViolation) {
		t.Fatalf("Create(max_input_chars merge) error = %v, want %v", err, ErrPolicyViolation)
	}
}

func TestMergeServicePoliciesConservativelyCombinesLayers(t *testing.T) {
	tenantPolicy := &repository.ServicePolicyConfig{
		Enabled: true,
		Request: &repository.GuardrailRuleSet{
			AllowModels:   []string{"public-model", "backup-model"},
			BlockModels:   []string{"blocked-a"},
			BlockTerms:    []string{"tenant-term"},
			RedactTerms:   []string{"tenant-secret"},
			MaxInputChars: 20,
		},
		Response: &repository.GuardrailRuleSet{
			BlockTerms:     []string{"tenant-out"},
			RedactTerms:    []string{"tenant-redact"},
			MaxOutputChars: 50,
		},
	}
	projectPolicy := &repository.ServicePolicyConfig{
		Request: &repository.GuardrailRuleSet{
			AllowModels:   []string{"public-model"},
			BlockModels:   []string{"blocked-b"},
			BlockTerms:    []string{"project-term"},
			RedactTerms:   []string{"project-secret"},
			MaxInputChars: 12,
		},
		Response: &repository.GuardrailRuleSet{
			BlockTerms:     []string{"project-out"},
			RedactTerms:    []string{"project-redact"},
			MaxOutputChars: 40,
		},
	}
	servicePolicy := &repository.ServicePolicyConfig{
		Request: &repository.GuardrailRuleSet{
			AllowModels:   []string{"public-model", "service-only"},
			BlockTerms:    []string{"service-term"},
			RedactTerms:   []string{"service-secret"},
			MaxInputChars: 8,
		},
		Response: &repository.GuardrailRuleSet{
			RedactTerms:    []string{"service-redact"},
			MaxOutputChars: 18,
		},
	}

	merged := mergeServicePolicies(nil, tenantPolicy)
	merged = mergeServicePolicies(merged, projectPolicy)
	merged = mergeServicePolicies(merged, servicePolicy)

	if merged == nil || !merged.Enabled {
		t.Fatalf("mergeServicePolicies() = %+v, want enabled merged policy", merged)
	}
	if merged.Request == nil || len(merged.Request.AllowModels) != 1 || merged.Request.AllowModels[0] != "public-model" {
		t.Fatalf("merged.Request.AllowModels = %#v, want intersection [public-model]", merged.Request)
	}
	if merged.Request.MaxInputChars != 8 {
		t.Fatalf("merged.Request.MaxInputChars = %d, want %d", merged.Request.MaxInputChars, 8)
	}
	if !containsString(merged.Request.BlockModels, "blocked-a") || !containsString(merged.Request.BlockModels, "blocked-b") {
		t.Fatalf("merged.Request.BlockModels = %#v, want union", merged.Request.BlockModels)
	}
	if !containsString(merged.Request.BlockTerms, "tenant-term") || !containsString(merged.Request.BlockTerms, "project-term") || !containsString(merged.Request.BlockTerms, "service-term") {
		t.Fatalf("merged.Request.BlockTerms = %#v, want union", merged.Request.BlockTerms)
	}
	if !containsString(merged.Request.RedactTerms, "tenant-secret") || !containsString(merged.Request.RedactTerms, "project-secret") || !containsString(merged.Request.RedactTerms, "service-secret") {
		t.Fatalf("merged.Request.RedactTerms = %#v, want union", merged.Request.RedactTerms)
	}
	if merged.Response == nil || merged.Response.MaxOutputChars != 18 {
		t.Fatalf("merged.Response.MaxOutputChars = %#v, want 18", merged.Response)
	}
	if !containsString(merged.Response.BlockTerms, "tenant-out") || !containsString(merged.Response.BlockTerms, "project-out") {
		t.Fatalf("merged.Response.BlockTerms = %#v, want union", merged.Response.BlockTerms)
	}
	if !containsString(merged.Response.RedactTerms, "tenant-redact") || !containsString(merged.Response.RedactTerms, "project-redact") || !containsString(merged.Response.RedactTerms, "service-redact") {
		t.Fatalf("merged.Response.RedactTerms = %#v, want union", merged.Response.RedactTerms)
	}
}

func TestReviewSubscriptionApprovesScopedKey(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chat-subscription",
			"object":  "chat.completion",
			"created": 1,
			"model":   "public-model",
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer upstream.Close()

	env := newCatalogTestEnv(t, []config.ProviderConfig{
		{Name: "openai-main", Type: "openai", BaseURL: upstream.URL, Endpoint: "chat", APIKey: "k1", Model: "public-model", Timeout: 5, Enabled: true, MaxTokens: 256},
	})
	createResult, err := env.service.CreateService(context.Background(), repository.CreateServiceParams{
		TenantID:        env.identity.TenantID,
		Name:            "Scoped Service",
		RequestPrefix:   "scoped-service",
		DefaultProvider: "openai-main",
		DefaultModel:    "public-model",
		Enabled:         true,
		Config: repository.ServiceConfig{
			Surfaces: []string{"responses", "invoke"},
			PromptTemplate: &repository.PromptTemplateConfig{
				UserTemplate: "Hello {{name}}",
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
	if _, _, err := env.service.PublishServiceVersion(context.Background(), env.identity.TenantID, createResult.Service.ID, createResult.InitialVersion.ID, "published"); err != nil {
		t.Fatalf("PublishServiceVersion() error: %v", err)
	}

	subscription, err := env.store.CreateServiceSubscription(context.Background(), env.identity.TenantID, repository.CreateServiceSubscriptionParams{
		ServiceID:             createResult.Service.ID,
		ConsumerName:          "consumer-a",
		ConsumerEmail:         "consumer@example.com",
		RequestedBudgetUSD:    3.5,
		RequestedRateLimitQPS: 6,
		AllowedSurfaces:       []string{"responses"},
	})
	if err != nil {
		t.Fatalf("CreateServiceSubscription() error: %v", err)
	}

	result, err := env.service.ReviewSubscription(context.Background(), env.identity.TenantID, subscription.ID, "approve", "ok")
	if err != nil {
		t.Fatalf("ReviewSubscription() error: %v", err)
	}
	if result.APIKey == nil || result.APISecret == "" {
		t.Fatalf("ReviewSubscription() = %+v, want issued api key and secret", result)
	}

	issuedIdentity, err := env.auth.Authenticate(context.Background(), result.APIKey.Key, result.APISecret)
	if err != nil {
		t.Fatalf("Authenticate(approved key) error: %v", err)
	}
	if len(issuedIdentity.APIKeyServices) != 1 || issuedIdentity.APIKeyServices[0] != "scoped-service" {
		t.Fatalf("approved key services = %#v, want [scoped-service]", issuedIdentity.APIKeyServices)
	}
	if len(issuedIdentity.APIKeyProviders) != 1 || issuedIdentity.APIKeyProviders[0] != "openai-main" {
		t.Fatalf("approved key providers = %#v, want [openai-main]", issuedIdentity.APIKeyProviders)
	}
	if len(issuedIdentity.APIKeyModels) != 1 || issuedIdentity.APIKeyModels[0] != "public-model" {
		t.Fatalf("approved key models = %#v, want [public-model]", issuedIdentity.APIKeyModels)
	}

	if _, _, err := env.service.CreatePromptInvocation(context.Background(), issuedIdentity, "scoped-service", PromptInvokeRequest{
		Variables: map[string]any{"name": "Gateyes"},
	}, "session-3"); !errors.Is(err, ErrServiceSurfaceDenied) {
		t.Fatalf("CreatePromptInvocation(disallowed surface) error = %v, want %v", err, ErrServiceSurfaceDenied)
	}
}
