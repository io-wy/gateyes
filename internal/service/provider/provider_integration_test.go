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
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		isStream := reqBody["stream"] == true

		switch r.URL.Path {
		case "/responses":
			if r.Header.Get("Authorization") != "Bearer upstream-key" {
				t.Fatalf("Authorization header = %q, want %q", r.Header.Get("Authorization"), "Bearer upstream-key")
			}
			w.Header().Set("Content-Type", "application/json")
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
		case "/chat/completions", "/v1/chat/completions":
			if isStream {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"provider-model\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"index\":0}]}\n\n")
				_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"provider-model\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\",\"index\":0}]}\n\n")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chat-1",
				"object":  "chat.completion",
				"created": 1,
				"model":   "provider-model",
				"choices": []map[string]any{{
					"message": map[string]any{
						"role":    "assistant",
						"content": "hello chat",
					},
					"finish_reason": "stop",
				}},
				"usage": map[string]any{
					"prompt_tokens":     1,
					"completion_tokens": 2,
					"total_tokens":      3,
				},
			})
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

	// Test stream via chat completions endpoint
	p.cfg.Endpoint = "chat"
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
	var foundText bool
	for _, e := range got {
		if e.Text() == "hello" {
			foundText = true
		}
	}
	if !foundText {
		t.Fatalf("openAIProvider.StreamResponse() events = %+v, want hello delta", got)
	}
}

func TestAnthropicProviderCreateAndStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		isStream := reqBody["stream"] == true
		fmt.Printf("DEBUG SERVER: path=%s stream=%v\n", r.URL.Path, isStream)

		if r.URL.Path != "/v1/messages" {
			t.Fatalf("anthropic path = %q, want %q", r.URL.Path, "/v1/messages")
		}
		if r.Header.Get("x-api-key") != "anthropic-key" {
			t.Fatalf("x-api-key header = %q, want %q", r.Header.Get("x-api-key"), "anthropic-key")
		}
		if isStream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "event: message_start\n")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"resp-1\",\"model\":\"claude-provider\",\"usage\":{\"input_tokens\":2}}}\n\n")
			_, _ = fmt.Fprint(w, "event: content_block_start\n")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"hello\"}}\n\n")
			_, _ = fmt.Fprint(w, "event: content_block_delta\n")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n")
			_, _ = fmt.Fprint(w, "event: content_block_start\n")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call-1\",\"name\":\"lookup\",\"input\":{\"city\":\"shanghai\"}}}\n\n")
			_, _ = fmt.Fprint(w, "event: content_block_stop\n")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_stop\",\"index\":1}\n\n")
			_, _ = fmt.Fprint(w, "event: message_delta\n")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"delta\":{},\"usage\":{\"output_tokens\":3}}\n\n")
			_, _ = fmt.Fprint(w, "event: message_stop\n")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
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
	if len(got) < 3 || got[0].Type != EventContentDelta || got[len(got)-1].Type != EventResponseCompleted {
		t.Fatalf("anthropicProvider.StreamResponse() events = %+v, want text/tool/completed sequence", got)
	}
}
