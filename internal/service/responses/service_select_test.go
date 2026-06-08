package responses

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestSelectProvider(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"p-a", "p-b"},
		providerConfigs: []config.ProviderConfig{
			{Name: "p-a", Type: "openai", BaseURL: up.URL, Endpoint: "chat", APIKey: "k", Model: "m1", Timeout: 5, Enabled: true, MaxTokens: 256},
			{Name: "p-b", Type: "openai", BaseURL: up.URL, Endpoint: "chat", APIKey: "k", Model: "m2", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	})

	p, err := env.service.selectProvider(context.Background(), env.identity, "s1", &provider.ResponseRequest{Model: "public-model"})
	if err != nil {
		t.Fatalf("selectProvider() error: %v", err)
	}
	if p == nil || (p.Name() != "p-a" && p.Name() != "p-b") {
		t.Fatalf("selectProvider() = %v, want p-a or p-b", p)
	}
}

func TestGetCandidateProviders(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"p-a", "p-b"},
		providerConfigs: []config.ProviderConfig{
			{Name: "p-a", Type: "openai", BaseURL: up.URL, Endpoint: "chat", APIKey: "k", Model: "m1", Timeout: 5, Enabled: true, MaxTokens: 256},
			{Name: "p-b", Type: "openai", BaseURL: up.URL, Endpoint: "chat", APIKey: "k", Model: "m2", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	})

	candidates := env.service.getCandidateProviders(context.Background(), env.identity, "s1", &provider.ResponseRequest{Model: "public-model"})
	if len(candidates) != 2 {
		t.Fatalf("getCandidateProviders() = %d, want 2", len(candidates))
	}
}

func TestBuildRouteContext(t *testing.T) {
	req := &provider.ResponseRequest{
		Model: "public-model",
		Messages: []provider.Message{{
			Role:    "user",
			Content: provider.TextBlocks("hello"),
		}},
		Stream: true,
		Tools:  []any{map[string]any{"type": "function"}},
		OutputFormat: &provider.OutputFormat{
			Type: "json_schema",
		},
	}
	ctx := buildRouteContext(req, "session-1")
	if ctx.Model != "public-model" || ctx.SessionID != "session-1" || !ctx.Stream {
		t.Fatalf("buildRouteContext() basic fields = %+v", ctx)
	}
	if !ctx.HasTools || !ctx.HasStructuredOutput {
		t.Fatalf("buildRouteContext() flags = %+v", ctx)
	}
	if ctx.PromptTokens <= 0 {
		t.Fatalf("buildRouteContext() prompt tokens = %d", ctx.PromptTokens)
	}
}

func TestNormalizeResponse(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	exec := &execution{
		provider:              env.providerMgr.List()[0],
		requestedModel:        "public-model",
		responseID:            "resp-norm",
		estimatedPromptTokens: 5,
	}
	resp := env.service.normalizeResponse(exec, &provider.Response{ID: "upstream-id", Status: "completed"})
	if resp.ID != "resp-norm" {
		t.Fatalf("normalizeResponse() ID = %q, want resp-norm", resp.ID)
	}
	if resp.Object != "response" {
		t.Fatalf("normalizeResponse() Object = %q, want response", resp.Object)
	}
	if resp.Model != "public-model" {
		t.Fatalf("normalizeResponse() Model = %q, want public-model", resp.Model)
	}
	if resp.Usage.PromptTokens != 5 {
		t.Fatalf("normalizeResponse() PromptTokens = %d, want 5", resp.Usage.PromptTokens)
	}
}

func TestNormalizeResponseFillsDefaultsForNil(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	exec := &execution{
		provider:              env.providerMgr.List()[0],
		requestedModel:        "public-model",
		responseID:            "resp-nil",
		estimatedPromptTokens: 3,
	}
	resp := env.service.normalizeResponse(exec, nil)
	if resp.ID != "resp-nil" || resp.Object != "response" || resp.Status != "completed" {
		t.Fatalf("normalizeResponse(nil) = %+v", resp)
	}
}
