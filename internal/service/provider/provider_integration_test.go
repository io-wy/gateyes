package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestOpenAIProviderCreateAndStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/responses":
			if r.Header.Get("Authorization") != "Bearer upstream-key" {
				t.Fatalf("Authorization header = %q, want %q", r.Header.Get("Authorization"), "Bearer upstream-key")
			}
			if r.Header.Get("Accept") == "text/event-stream" {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hi\"}\n\n")
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"call-1\",\"type\":\"function_call\",\"name\":\"lookup\",\"arguments\":\"{}\"}}\n\n")
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-1\",\"created_at\":1,\"model\":\"provider-model\",\"status\":\"completed\",\"output\":[{\"id\":\"msg-1\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"hi\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"total_tokens\":3}}}\n\n")
				_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "resp-1",
				"created_at": 1,
				"model":      "provider-model",
				"status":     "completed",
				"output": []map[string]any{{
					"id":     "msg-1",
					"type":   "message",
					"role":   "assistant",
					"status": "completed",
					"content": []map[string]any{{
						"type": "output_text",
						"text": "hello",
					}},
				}},
				"usage": map[string]any{
					"input_tokens":  1,
					"output_tokens": 2,
					"total_tokens":  3,
				},
			})
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"provider-model\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
			_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	p := NewOpenAIProvider(config.ProviderConfig{
		Name:      "openai-a",
		Type:      "openai",
		BaseURL:   upstream.URL,
		APIKey:    "upstream-key",
		Model:     "provider-model",
		Timeout:   5,
		Endpoint:  "responses",
		MaxTokens: 128,
	}).(*openAIProvider)

	resp, err := p.CreateResponse(context.Background(), &ResponseRequest{
		Model: "public-model",
		Input: "hello",
	})
	if err != nil {
		t.Fatalf("openAIProvider.CreateResponse() error: %v", err)
	}
	if resp.Model != "public-model" || resp.OutputText() != "hello" {
		t.Fatalf("openAIProvider.CreateResponse() = %+v, want normalized response", resp)
	}

	events, errs := p.StreamResponse(context.Background(), &ResponseRequest{
		Model:  "public-model",
		Input:  "hello",
		Stream: true,
	})
	var got []ResponseEvent
	for event := range events {
		got = append(got, event)
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("openAIProvider.StreamResponse() error: %v", err)
		}
	}
	if len(got) != 3 || got[0].Type != "response.output_text.delta" || got[2].Type != "response.completed" {
		t.Fatalf("openAIProvider.StreamResponse() events = %+v, want delta/item/completed sequence", got)
	}
}

func TestAnthropicProviderCreateAndStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("anthropic path = %q, want %q", r.URL.Path, "/v1/messages")
		}
		if r.Header.Get("x-api-key") != "anthropic-key" {
			t.Fatalf("x-api-key header = %q, want %q", r.Header.Get("x-api-key"), "anthropic-key")
		}
		if r.Header.Get("Accept") == "text/event-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "event: message_start\n")
			_, _ = fmt.Fprint(w, "data: {\"message\":{\"id\":\"resp-1\",\"model\":\"claude-provider\",\"usage\":{\"input_tokens\":2}}}\n\n")
			_, _ = fmt.Fprint(w, "event: content_block_start\n")
			_, _ = fmt.Fprint(w, "data: {\"content_block\":{\"type\":\"text\",\"text\":\"hello\"}}\n\n")
			_, _ = fmt.Fprint(w, "event: content_block_delta\n")
			_, _ = fmt.Fprint(w, "data: {\"delta\":{\"text\":\" world\"}}\n\n")
			_, _ = fmt.Fprint(w, "event: content_block_start\n")
			_, _ = fmt.Fprint(w, "data: {\"content_block\":{\"type\":\"tool_use\",\"id\":\"call-1\",\"name\":\"lookup\",\"input\":{\"city\":\"shanghai\"}}}\n\n")
			_, _ = fmt.Fprint(w, "event: content_block_stop\n")
			_, _ = fmt.Fprint(w, "data: {}\n\n")
			_, _ = fmt.Fprint(w, "event: message_delta\n")
			_, _ = fmt.Fprint(w, "data: {\"usage\":{\"output_tokens\":3}}\n\n")
			_, _ = fmt.Fprint(w, "event: message_stop\n")
			_, _ = fmt.Fprint(w, "data: {}\n\n")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "resp-1",
			"model": "claude-provider",
			"role":  "assistant",
			"content": []map[string]any{
				{"type": "text", "text": "hello"},
				{"type": "tool_use", "id": "call-1", "name": "lookup", "input": map[string]any{"city": "shanghai"}},
			},
			"usage": map[string]any{
				"input_tokens":  2,
				"output_tokens": 3,
			},
		})
	}))
	defer upstream.Close()

	p := NewAnthropicProvider(config.ProviderConfig{
		Name:      "anthropic-a",
		Type:      "anthropic",
		BaseURL:   upstream.URL,
		APIKey:    "anthropic-key",
		Model:     "claude-provider",
		Timeout:   5,
		MaxTokens: 256,
	}).(*anthropicProvider)

	resp, err := p.CreateResponse(context.Background(), &ResponseRequest{
		Model: "claude-public",
		Input: []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if err != nil {
		t.Fatalf("anthropicProvider.CreateResponse() error: %v", err)
	}
	if resp.Model != "claude-public" || resp.Output[1].Type != "function_call" {
		t.Fatalf("anthropicProvider.CreateResponse() = %+v, want normalized response", resp)
	}

	events, errs := p.StreamResponse(context.Background(), &ResponseRequest{
		Model:  "claude-public",
		Input:  []any{map[string]any{"role": "user", "content": "hello"}},
		Stream: true,
	})
	var got []ResponseEvent
	for event := range events {
		got = append(got, event)
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("anthropicProvider.StreamResponse() error: %v", err)
		}
	}
	if len(got) < 3 || got[0].Type != "response.output_text.delta" || got[len(got)-1].Type != "response.completed" {
		t.Fatalf("anthropicProvider.StreamResponse() events = %+v, want text/tool/completed sequence", got)
	}
}
