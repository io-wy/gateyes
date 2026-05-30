package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestAnthropicClientAdditionalErrorBranches(t *testing.T) {
	statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "anthropic boom", http.StatusBadGateway)
	}))
	defer statusServer.Close()

	p := NewAnthropicProvider(config.ProviderConfig{
		Name:      "anthropic-a",
		Type:      "anthropic",
		BaseURL:   statusServer.URL,
		APIKey:    "anthropic-key",
		Model:     "claude-test",
		Timeout:   5,
		MaxTokens: 128,
	}).(*anthropicProvider)
	if _, err := p.CreateResponse(context.Background(), &ResponseRequest{
		Model:    "claude-public",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	}); err == nil {
		t.Fatal("Anthropic CreateResponse(status error) error = nil, want upstream error")
	}

	badJSONServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":`))
	}))
	defer badJSONServer.Close()

	p = NewAnthropicProvider(config.ProviderConfig{
		Name:      "anthropic-a",
		Type:      "anthropic",
		BaseURL:   badJSONServer.URL,
		APIKey:    "anthropic-key",
		Model:     "claude-test",
		Timeout:   5,
		MaxTokens: 128,
	}).(*anthropicProvider)
	if _, err := p.CreateResponse(context.Background(), &ResponseRequest{
		Model:    "claude-public",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	}); err == nil {
		t.Fatal("Anthropic CreateResponse(invalid json) error = nil, want parse error")
	}

	streamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "anthropic stream boom", http.StatusBadGateway)
	}))
	defer streamServer.Close()

	p = NewAnthropicProvider(config.ProviderConfig{
		Name:      "anthropic-a",
		Type:      "anthropic",
		BaseURL:   streamServer.URL,
		APIKey:    "anthropic-key",
		Model:     "claude-test",
		Timeout:   5,
		MaxTokens: 128,
	}).(*anthropicProvider)
	events, errs := p.StreamResponse(context.Background(), &ResponseRequest{
		Model:    "claude-public",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
		Stream:   true,
	})
	for range events {
	}
	var gotErr error
	for err := range errs {
		if err != nil {
			gotErr = err
		}
	}
	if gotErr == nil {
		t.Fatal("Anthropic StreamResponse(status error) err = nil, want upstream error")
	}

	badURLProvider := &anthropicProvider{baseProvider: newBaseProvider(config.ProviderConfig{
		Name:      "anthropic-a",
		Type:      "anthropic",
		BaseURL:   "://bad-url",
		APIKey:    "anthropic-key",
		Model:     "claude-test",
		Timeout:   5,
		MaxTokens: 128,
	})}
	if _, err := badURLProvider.newRequest(context.Background(), map[string]any{"model": "claude-test"}, false); err == nil {
		t.Fatal("anthropic newRequest(bad url) error = nil, want config error")
	}
	events, errs = badURLProvider.StreamResponse(context.Background(), &ResponseRequest{
		Model:    "claude-public",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
		Stream:   true,
	})
	for range events {
	}
	streamErr := error(nil)
	for err := range errs {
		if err != nil {
			streamErr = err
		}
	}
	if streamErr == nil {
		t.Fatal("Anthropic StreamResponse(bad url) err = nil, want config error")
	}

	transportServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	transportURL := transportServer.URL
	transportServer.Close()

	p = NewAnthropicProvider(config.ProviderConfig{
		Name:      "anthropic-a",
		Type:      "anthropic",
		BaseURL:   transportURL,
		APIKey:    "anthropic-key",
		Model:     "claude-test",
		Timeout:   1,
		MaxTokens: 128,
	}).(*anthropicProvider)
	if _, err := p.CreateResponse(context.Background(), &ResponseRequest{
		Model:    "claude-public",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	}); err == nil {
		t.Fatal("Anthropic CreateResponse(transport error) error = nil, want transport error")
	}
	events, errs = p.StreamResponse(context.Background(), &ResponseRequest{
		Model:    "claude-public",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
		Stream:   true,
	})
	for range events {
	}
	streamErr = nil
	for err := range errs {
		if err != nil {
			streamErr = err
		}
	}
	if streamErr == nil {
		t.Fatal("Anthropic StreamResponse(transport error) err = nil, want transport error")
	}
}
