package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestOpenAIClientAdditionalErrorBranches(t *testing.T) {
	statusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream boom"}}`))
	}))
	defer statusServer.Close()

	p := NewOpenAIProvider(config.ProviderConfig{
		Name:    "openai-a",
		Type:    "openai",
		BaseURL: statusServer.URL,
		APIKey:  "test-key",
		Model:   "provider-model",
		Timeout: 5,
	}).(*openAIProvider)

	if _, err := p.CreateResponse(context.Background(), &ResponseRequest{
		Model:    "public-model",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	}); err == nil {
		t.Fatal("CreateResponse(status error) error = nil, want upstream error")
	}

	chatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chat-1","object":"chat.completion","created":1,"model":"provider-model","choices":[{"message":{"role":"assistant","content":"hello chat"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`))
	}))
	defer chatServer.Close()

	p = NewOpenAIProvider(config.ProviderConfig{
		Name:     "openai-a",
		Type:     "openai",
		BaseURL:  chatServer.URL,
		APIKey:   "test-key",
		Model:    "provider-model",
		Timeout:  5,
		Endpoint: "chat",
	}).(*openAIProvider)
	resp, err := p.CreateResponse(context.Background(), &ResponseRequest{
		Model:    "public-model",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	})
	if err != nil || resp == nil || resp.OutputText() != "hello chat" {
		t.Fatalf("CreateResponse(chat endpoint) = (%+v,%v), want hello chat", resp, err)
	}

	badJSONServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":`))
	}))
	defer badJSONServer.Close()

	p = NewOpenAIProvider(config.ProviderConfig{
		Name:     "openai-a",
		Type:     "openai",
		BaseURL:  badJSONServer.URL,
		APIKey:   "test-key",
		Model:    "provider-model",
		Timeout:  5,
		Endpoint: "responses",
	}).(*openAIProvider)
	if _, err := p.CreateResponse(context.Background(), &ResponseRequest{
		Model:    "public-model",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	}); err == nil {
		t.Fatal("CreateResponse(invalid json) error = nil, want parse error")
	}

	badURLProvider := &openAIProvider{baseProvider: newBaseProvider(config.ProviderConfig{
		Name:    "openai-a",
		Type:    "openai",
		BaseURL: "://bad-url",
		APIKey:  "test-key",
		Model:   "provider-model",
		Timeout: 5,
	})}
	if _, err := badURLProvider.CreateResponse(context.Background(), &ResponseRequest{
		Model:    "public-model",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	}); err == nil {
		t.Fatal("CreateResponse(bad url) error = nil, want config error")
	}
	events, errs := badURLProvider.StreamResponse(context.Background(), &ResponseRequest{
		Model:    "public-model",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
		Stream:   true,
	})
	for range events {
	}
	var streamErr error
	for err := range errs {
		if err != nil {
			streamErr = err
		}
	}
	if streamErr == nil {
		t.Fatal("StreamResponse(bad url) err = nil, want config error")
	}

	transportServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	transportURL := transportServer.URL
	transportServer.Close()

	p = NewOpenAIProvider(config.ProviderConfig{
		Name:    "openai-a",
		Type:    "openai",
		BaseURL: transportURL,
		APIKey:  "test-key",
		Model:   "provider-model",
		Timeout: 1,
	}).(*openAIProvider)
	if _, err := p.CreateResponse(context.Background(), &ResponseRequest{
		Model:    "public-model",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	}); err == nil {
		t.Fatal("CreateResponse(transport error) error = nil, want transport error")
	}
	events, errs = p.StreamResponse(context.Background(), &ResponseRequest{
		Model:    "public-model",
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
		t.Fatal("StreamResponse(transport error) err = nil, want transport error")
	}
}
