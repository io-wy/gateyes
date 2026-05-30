package provider

import (
	"net/http"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestProviderHTTPClientsUseExplicitConnectionPools(t *testing.T) {
	openaiProvider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "openai-a",
		Type:    "openai",
		BaseURL: "https://openai.example",
		APIKey:  "test-key",
		Model:   "gpt-test",
		Timeout: 5,
	}).(*openAIProvider)
	openaiTransport, ok := openaiProvider.client.Transport.(*http.Transport)
	if !ok || openaiTransport == nil {
		t.Fatalf("openai client transport = %T, want *http.Transport", openaiProvider.client.Transport)
	}
	if openaiTransport.MaxIdleConns <= 0 || openaiTransport.MaxIdleConnsPerHost <= 0 || openaiTransport.IdleConnTimeout <= 0 {
		t.Fatalf("openai transport pool = %+v, want explicit positive pool settings", openaiTransport)
	}

	anthropicProvider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "anthropic-a",
		Type:    "anthropic",
		BaseURL: "https://anthropic.example",
		APIKey:  "anthropic-key",
		Model:   "claude-test",
		Timeout: 5,
	}).(*anthropicProvider)
	anthropicTransport, ok := anthropicProvider.client.Transport.(*http.Transport)
	if !ok || anthropicTransport == nil {
		t.Fatalf("anthropic client transport = %T, want *http.Transport", anthropicProvider.client.Transport)
	}
	if anthropicTransport.MaxIdleConns <= 0 || anthropicTransport.MaxIdleConnsPerHost <= 0 || anthropicTransport.IdleConnTimeout <= 0 {
		t.Fatalf("anthropic transport pool = %+v, want explicit positive pool settings", anthropicTransport)
	}
}
