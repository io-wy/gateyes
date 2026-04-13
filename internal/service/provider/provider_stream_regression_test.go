package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestParseOpenAIResponseEventHandlesResponsesFailure(t *testing.T) {
	event, err := parseOpenAIResponseEvent(`{"type":"response.failed","response":{"error":{"message":"upstream exploded"}}}`, "public-model")
	if err == nil || err.Error() != "upstream exploded" {
		t.Fatalf("parseOpenAIResponseEvent(response.failed) = (%+v,%v), want upstream exploded error", event, err)
	}
	if event != nil {
		t.Fatalf("parseOpenAIResponseEvent(response.failed) event = %+v, want nil", event)
	}
}

func TestConvertOpenAIResponseAcceptsNullOutput(t *testing.T) {
	resp := convertOpenAIResponse(openAIResponsePayload{
		ID:        "resp-null-output",
		Object:    "response",
		CreatedAt: 123,
		Model:     "provider-model",
		Status:    "completed",
		Output:    nil,
	}, "public-model")

	if resp == nil {
		t.Fatal("convertOpenAIResponse(nil output) = nil, want normalized response")
	}
	if resp.Model != "public-model" || resp.Status != "completed" {
		t.Fatalf("convertOpenAIResponse(nil output) = %+v, want preserved model/status", resp)
	}
	if len(resp.Output) != 0 {
		t.Fatalf("convertOpenAIResponse(nil output) output = %+v, want empty slice", resp.Output)
	}
}

func TestAnthropicBuildParamsUsesTypedOptionsAndRawFallback(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{
		Name:      "anthropic-a",
		Type:      "anthropic",
		BaseURL:   "https://anthropic.example",
		APIKey:    "anthropic-key",
		Model:     "claude-test",
		Timeout:   5,
		MaxTokens: 256,
	}).(*anthropicProvider)

	params, err := p.buildParams(&ResponseRequest{
		Model: "claude-public",
		Messages: []Message{{
			Role:    "user",
			Content: TextBlocks("hello"),
		}},
		Options: &RequestOptions{
			System: "be concise",
			Thinking: &AnthropicThinking{
				Type:         "enabled",
				BudgetTokens: 32,
			},
			CacheControl: &AnthropicCacheControl{
				Type: "ephemeral",
				TTL:  "10m",
			},
			Raw: map[string]any{
				"metadata": map[string]any{"suite": "regression"},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildParams() error: %v", err)
	}
	if params["system"] != "be concise" {
		t.Fatalf("buildParams() system = %#v, want typed system option", params["system"])
	}
	thinking, ok := params["thinking"].(*AnthropicThinking)
	if !ok || thinking.BudgetTokens != 32 {
		t.Fatalf("buildParams() thinking = %#v, want typed thinking config", params["thinking"])
	}
	cacheControl, ok := params["cache_control"].(*AnthropicCacheControl)
	if !ok || cacheControl.TTL != "10m" {
		t.Fatalf("buildParams() cache_control = %#v, want typed cache control", params["cache_control"])
	}
	metadata, ok := params["metadata"].(map[string]any)
	if !ok || metadata["suite"] != "regression" {
		t.Fatalf("buildParams() raw fallback = %#v, want metadata passthrough", params["metadata"])
	}
}

func TestParseAnthropicStreamEventEmitsThinkingDelta(t *testing.T) {
	state := &anthropicStreamState{
		responseID: "resp-1",
		model:      "claude-test",
	}

	event := parseAnthropicStreamEvent("content_block_delta", `{"delta":{"type":"thinking_delta","thinking":"step by step"}}`, state)
	if event == nil {
		t.Fatal("parseAnthropicStreamEvent(thinking_delta) = nil, want thinking event")
	}
	if event.Type != EventThinkingDelta || event.ThinkingDelta != "step by step" {
		t.Fatalf("parseAnthropicStreamEvent(thinking_delta) = %+v, want thinking_delta payload", event)
	}
}

func TestOpenAIProviderNewRequestAppliesVendorProfileHeadersAndExtraBody(t *testing.T) {
	p := NewOpenAIProvider(config.ProviderConfig{
		Name:     "openai-vllm",
		Type:     "openai",
		Vendor:   "vllm",
		BaseURL:  "https://openai.example/v1",
		Endpoint: "chat",
		APIKey:   "test-key",
		Model:    "qwen-public",
		Timeout:  5,
		Headers: map[string]string{
			"X-Custom-Provider": "gateyes",
		},
		ExtraBody: map[string]any{
			"temperature": 0.25,
		},
	}).(*openAIProvider)

	httpReq, err := p.newRequest(context.Background(), &ResponseRequest{
		Model: "qwen-public",
		Messages: []Message{{
			Role:    "user",
			Content: TextBlocks("hello"),
		}},
	}, false)
	if err != nil {
		t.Fatalf("newRequest() error: %v", err)
	}

	if got := httpReq.Header.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("Authorization header = %q, want Bearer token", got)
	}
	if got := httpReq.Header.Get("X-Custom-Provider"); got != "gateyes" {
		t.Fatalf("custom header = %q, want gateyes", got)
	}
	if got := httpReq.Header.Get("X-Gateyes-Vendor"); got != "vllm" {
		t.Fatalf("vendor header = %q, want vllm", got)
	}

	var payload map[string]any
	if err := json.NewDecoder(httpReq.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["temperature"] != 0.25 {
		t.Fatalf("extra body temperature = %#v, want 0.25", payload["temperature"])
	}
	if payload["top_k"] != float64(-1) {
		t.Fatalf("vendor profile top_k = %#v, want -1 for vllm", payload["top_k"])
	}
}

func TestAnthropicBuildParamsAppliesVendorProfileAndExtraBody(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{
		Name:      "anthropic-minimax",
		Type:      "anthropic",
		Vendor:    "minimax",
		BaseURL:   "https://anthropic.example",
		APIKey:    "anthropic-key",
		Model:     "MiniMax-M2.5",
		Timeout:   5,
		MaxTokens: 256,
		ExtraBody: map[string]any{
			"temperature": 0.7,
		},
	}).(*anthropicProvider)

	params, err := p.buildParams(&ResponseRequest{
		Model: "MiniMax-M2.5",
		Messages: []Message{{
			Role:    "user",
			Content: TextBlocks("hello"),
		}},
	})
	if err != nil {
		t.Fatalf("buildParams() error: %v", err)
	}
	if params["temperature"] != 0.7 {
		t.Fatalf("extra body temperature = %#v, want 0.7", params["temperature"])
	}
	if params["stream_options"] == nil {
		t.Fatalf("vendor profile stream_options = nil, want minimax-specific default")
	}
}

func TestAnthropicProviderCreateResponseAppliesConfiguredHeaders(t *testing.T) {
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	p := NewAnthropicProvider(config.ProviderConfig{
		Name:      "anthropic-a",
		Type:      "anthropic",
		BaseURL:   server.URL,
		APIKey:    "anthropic-key",
		Model:     "claude-test",
		Timeout:   5,
		MaxTokens: 128,
		Headers: map[string]string{
			"Anthropic-Beta": "tools-2024-04-04",
		},
	}).(*anthropicProvider)

	_, err := p.CreateResponse(context.Background(), &ResponseRequest{
		Model: "claude-test",
		Messages: []Message{{
			Role:    "user",
			Content: TextBlocks("hello"),
		}},
	})
	if err != nil {
		t.Fatalf("CreateResponse() error: %v", err)
	}
	if got := gotHeader.Get("Anthropic-Beta"); got != "tools-2024-04-04" {
		t.Fatalf("Anthropic-Beta header = %q, want configured header", got)
	}
}
