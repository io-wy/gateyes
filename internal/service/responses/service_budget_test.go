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

func TestEnsureQuotaAvailableReturnsNilWhenQuotaSufficient(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})
	env.identity.Quota = 100
	env.identity.Used = 0

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
}

func TestEnsureQuotaAvailableReturnsQuotaExceededWhenOverLimit(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":10,"total_tokens":12}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})
	env.identity.Quota = 5
	env.identity.Used = 0

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if !errors.Is(err, auth.ErrQuotaExceeded) {
		t.Fatalf("Create() error = %v, want %v", err, auth.ErrQuotaExceeded)
	}
}

func TestValidateVisibleOutputBudgetReturnsNilForVisibleOutput(t *testing.T) {
	exec := &execution{
		upstreamRequest: &provider.ResponseRequest{MaxOutputTokens: 64},
	}
	resp := &provider.Response{
		Output: []provider.ResponseOutput{{
			Type: "message",
			Content: []provider.ResponseContent{{
				Type: "output_text",
				Text: "hello",
			}},
		}},
	}
	if err := validateVisibleOutputBudget(exec, resp); err != nil {
		t.Fatalf("validateVisibleOutputBudget() = %v, want nil", err)
	}
}

func TestValidateVisibleOutputBudgetReturnsErrorWhenNoVisibleOutput(t *testing.T) {
	exec := &execution{
		upstreamRequest: &provider.ResponseRequest{MaxOutputTokens: 64},
	}
	resp := &provider.Response{
		Usage: provider.Usage{CompletionTokens: 2},
	}
	if err := validateVisibleOutputBudget(exec, resp); err == nil {
		t.Fatal("validateVisibleOutputBudget() = nil, want error")
	} else if !errors.Is(err, ErrOutputBudgetTooLow) {
		t.Fatalf("validateVisibleOutputBudget() = %v, want ErrOutputBudgetTooLow", err)
	}
}

func TestValidateVisibleOutputBudgetReturnsNilWhenNoLimit(t *testing.T) {
	exec := &execution{
		upstreamRequest: &provider.ResponseRequest{},
	}
	resp := &provider.Response{}
	if err := validateVisibleOutputBudget(exec, resp); err != nil {
		t.Fatalf("validateVisibleOutputBudget() = %v, want nil when no limit", err)
	}
}

func TestValidateVisibleOutputBudgetAllowsThinkingOnlyIfNotNearLimit(t *testing.T) {
	exec := &execution{
		upstreamRequest: &provider.ResponseRequest{MaxOutputTokens: 100},
	}
	resp := &provider.Response{
		Output: []provider.ResponseOutput{{
			Type: "message",
			Content: []provider.ResponseContent{{
				Type:     "thinking",
				Thinking: "reasoning",
			}},
		}},
		Usage: provider.Usage{CompletionTokens: 50},
	}
	if err := validateVisibleOutputBudget(exec, resp); err != nil {
		t.Fatalf("validateVisibleOutputBudget() = %v, want nil for thinking under limit", err)
	}
}

func TestValidateVisibleOutputBudgetRejectsThinkingOnlyNearLimit(t *testing.T) {
	exec := &execution{
		upstreamRequest: &provider.ResponseRequest{MaxOutputTokens: 100},
	}
	resp := &provider.Response{
		Output: []provider.ResponseOutput{{
			Type: "message",
			Content: []provider.ResponseContent{{
				Type:     "thinking",
				Thinking: "reasoning",
			}},
		}},
		Usage: provider.Usage{CompletionTokens: 95},
	}
	if err := validateVisibleOutputBudget(exec, resp); err == nil {
		t.Fatal("validateVisibleOutputBudget() = nil, want error for thinking near limit")
	}
}

func TestHasVisibleOutputReturnsTrueForText(t *testing.T) {
	resp := &provider.Response{
		Output: []provider.ResponseOutput{{
			Content: []provider.ResponseContent{{Type: "output_text", Text: "hi"}},
		}},
	}
	if !hasVisibleOutput(resp) {
		t.Fatal("hasVisibleOutput() = false, want true")
	}
}

func TestHasVisibleOutputReturnsFalseForEmpty(t *testing.T) {
	if hasVisibleOutput(nil) {
		t.Fatal("hasVisibleOutput(nil) = true, want false")
	}
}

func TestNearOutputBudgetLimitReturnsTrueAt90Percent(t *testing.T) {
	if !nearOutputBudgetLimit(90, 100) {
		t.Fatal("nearOutputBudgetLimit(90,100) = false, want true")
	}
}

func TestNearOutputBudgetLimitReturnsFalseBelowThreshold(t *testing.T) {
	if nearOutputBudgetLimit(80, 100) {
		t.Fatal("nearOutputBudgetLimit(80,100) = true, want false")
	}
}

func TestRecordBudgetExceededUpdatesResponseAndReturnsError(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	exec := &execution{
		provider:        env.providerMgr.List()[0],
		requestedModel:  "public-model",
		upstreamRequest: &provider.ResponseRequest{Model: "public-model"},
		responseID:      "resp-budget",
		routeTrace:      &routeTrace{},
	}
	resp := &provider.Response{Usage: provider.Usage{TotalTokens: 10}}
	err := env.service.recordBudgetExceeded(context.Background(), env.identity, exec, resp, 100, "test-openai", 0.01)
	if !errors.Is(err, auth.ErrBudgetExceeded) {
		t.Fatalf("recordBudgetExceeded() = %v, want ErrBudgetExceeded", err)
	}
}

func TestCreateReturnsFallbackCountWhenMultipleProvidersFail(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"fail-a", "fail-b", "test-openai"},
		providerConfigs: []config.ProviderConfig{
			{Name: "fail-a", Type: "openai", BaseURL: "http://127.0.0.1:1", Endpoint: "chat", APIKey: "k", Model: "m1", Timeout: 1, Enabled: true, MaxTokens: 256},
			{Name: "fail-b", Type: "openai", BaseURL: "http://127.0.0.1:1", Endpoint: "chat", APIKey: "k", Model: "m2", Timeout: 1, Enabled: true, MaxTokens: 256},
			{Name: "test-openai", Type: "openai", BaseURL: up.URL, Endpoint: "chat", APIKey: "k", Model: "m", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	})

	result, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if result.Fallback != 2 {
		t.Fatalf("Create() fallback = %d, want 2", result.Fallback)
	}
}
