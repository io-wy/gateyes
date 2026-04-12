package responses

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestCreateReturnsQuotaExceededAfterUpstreamSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"chatcmpl-upstream","object":"chat.completion","created":1700000000,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`))
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})
	env.identity.Quota = 1
	env.identity.Used = 0

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if !errors.Is(err, auth.ErrQuotaExceeded) {
		t.Fatalf("Service.Create(quota exceeded) error = %v, want %v", err, auth.ErrQuotaExceeded)
	}
}

func TestCreateReturnsOutputBudgetTooLowWhenOnlyThinkingIsProduced(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"resp-upstream","created_at":1700000000,"model":"provider-model","status":"completed","output":[{"id":"msg-1","type":"message","role":"assistant","status":"completed","content":[{"type":"thinking","thinking":"internal reasoning","signature":"sig-1"}]}],"usage":{"input_tokens":3,"output_tokens":60,"total_tokens":63}}`))
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "responses",
		providers:   []string{"test-openai"},
	})

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model:           "public-model",
		Input:           "hello",
		MaxOutputTokens: 64,
	}, "")
	if !errors.Is(err, ErrOutputBudgetTooLow) {
		t.Fatalf("Service.Create(only thinking) error = %v, want %v", err, ErrOutputBudgetTooLow)
	}
}

func TestWrapErrorAndGinError(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
		wantType   string
	}{
		{err: auth.ErrModelNotAllowed, wantStatus: 403, wantType: "invalid_request_error"},
		{err: auth.ErrQuotaExceeded, wantStatus: 429, wantType: "rate_limit_error"},
		{err: ErrOutputBudgetTooLow, wantStatus: 400, wantType: "invalid_request_error"},
		{err: ErrNoProvider, wantStatus: 503, wantType: "internal_error"},
		{err: errors.New("boom"), wantStatus: 500, wantType: "internal_error"},
	}

	for _, tt := range tests {
		got := WrapError(tt.err)
		if got.Status != tt.wantStatus || got.Type != tt.wantType {
			t.Fatalf("WrapError(%v) = %+v, want status=%d type=%q", tt.err, got, tt.wantStatus, tt.wantType)
		}
		if got.Error() == "" {
			t.Fatalf("WrapError(%v).Error() = empty, want non-empty", tt.err)
		}
	}
}

func TestBuildRouteContextExtractsRoutingFeatures(t *testing.T) {
	req := &provider.ResponseRequest{
		Model: "public-model",
		Messages: []provider.Message{{
			Role: "user",
			Content: []provider.ContentBlock{
				{Type: "text", Text: "Please debug this Go stack trace"},
				{Type: "image", Image: &provider.ContentImage{URL: "https://example.com/a.png"}},
			},
		}},
		Stream: true,
		Tools:  []any{map[string]any{"type": "function"}},
		OutputFormat: &provider.OutputFormat{
			Type: "json_schema",
		},
	}

	ctx := buildRouteContext(req, "session-1")
	if ctx.Model != "public-model" || ctx.SessionID != "session-1" || !ctx.Stream {
		t.Fatalf("buildRouteContext() basic fields = %+v, want model/session/stream", ctx)
	}
	if !ctx.HasTools || !ctx.HasImages || !ctx.HasStructuredOutput {
		t.Fatalf("buildRouteContext() feature flags = %+v, want tools/images/structured_output", ctx)
	}
	if ctx.InputText == "" || ctx.PromptTokens <= 0 {
		t.Fatalf("buildRouteContext() text/tokens = %+v, want non-empty text and prompt tokens", ctx)
	}
}

func TestGetCandidateProvidersAppliesRuleEngine(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: "https://openai.example",
		providers:   []string{"general", "coder"},
		providerConfigs: []config.ProviderConfig{
			{
				Name:      "general",
				Type:      "openai",
				BaseURL:   "https://openai.example",
				Endpoint:  "chat",
				APIKey:    "upstream-key",
				Model:     "general-model",
				Timeout:   5,
				Enabled:   true,
				MaxTokens: 256,
			},
			{
				Name:      "coder",
				Type:      "openai",
				BaseURL:   "https://openai.example",
				Endpoint:  "chat",
				APIKey:    "upstream-key",
				Model:     "coder-model",
				Timeout:   5,
				Enabled:   true,
				MaxTokens: 256,
			},
		},
		routerConfig: config.RouterConfig{
			Strategy: "least_load",
			RuleEngine: config.RuleEngineConfig{
				Enabled: true,
				Rules: []config.RouteRuleConfig{{
					Name: "code-traffic",
					Match: config.RouteMatchConfig{
						HasTools: boolPtr(true),
						AnyRegex: []string{`(?i)stack trace`, `(?i)golang`},
					},
					Action: config.RouteActionConfig{
						Providers: []string{"coder"},
					},
				}},
			},
		},
	})

	req := &provider.ResponseRequest{
		Model: "public-model",
		Messages: []provider.Message{{
			Role:    "user",
			Content: provider.TextBlocks("Please debug this Go stack trace"),
		}},
		Tools: []any{map[string]any{"type": "function"}},
	}

	candidates := env.service.getCandidateProviders(context.Background(), env.identity, "session-1", req)
	if len(candidates) != 1 || candidates[0].Name() != "coder" {
		t.Fatalf("getCandidateProviders() = %v, want [coder]", providerNames(candidates))
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func providerNames(providers []provider.Provider) []string {
	result := make([]string, 0, len(providers))
	for _, p := range providers {
		result = append(result, p.Name())
	}
	return result
}
