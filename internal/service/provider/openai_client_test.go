package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
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

func TestOpenAIResponsesSendsPromptCacheKey(t *testing.T) {
	var upstreamBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp-1","object":"response","created_at":1,"model":"provider-model","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	}))
	defer upstream.Close()

	p := NewOpenAIProvider(config.ProviderConfig{
		Name:     "openai-a",
		Type:     "openai",
		BaseURL:  upstream.URL,
		APIKey:   "test-key",
		Model:    "provider-model",
		Endpoint: "responses",
		Timeout:  5,
	}).(*openAIProvider)

	if _, err := p.CreateResponse(context.Background(), &ResponseRequest{
		Model:          "public-model",
		Messages:       []Message{{Role: "user", Content: TextBlocks("hello")}},
		PromptCacheKey: "host:client",
	}); err != nil {
		t.Fatalf("CreateResponse() error: %v", err)
	}
	if got := upstreamBody["prompt_cache_key"]; got != "host:client" {
		t.Fatalf("prompt_cache_key = %v, want host:client", got)
	}
}

func TestOpenAIProviderCreateEmbeddingAlias(t *testing.T) {
	var upstreamBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("path = %q, want /embeddings", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{{
				"object":    "embedding",
				"index":     0,
				"embedding": []float64{0.1},
			}},
			"model": "text-embedding-3",
			"usage": map[string]any{"prompt_tokens": 1, "total_tokens": 1},
		})
	}))
	defer upstream.Close()

	p := NewOpenAIProvider(config.ProviderConfig{
		Name:    "test-openai",
		Type:    "openai",
		BaseURL: upstream.URL,
		APIKey:  "test-key",
		Timeout: 5,
		ModelAliases: map[string]string{
			"embed-alias": "text-embedding-3",
		},
	}).(*openAIProvider)

	_, err := p.CreateEmbedding(context.Background(), &EmbeddingRequest{
		Model: "embed-alias",
		Input: json.RawMessage(`"hello"`),
	})
	if err != nil {
		t.Fatalf("CreateEmbedding() error: %v", err)
	}
	if upstreamBody["model"] != "text-embedding-3" {
		t.Fatalf("expected alias resolved to text-embedding-3, got %v", upstreamBody["model"])
	}
}
