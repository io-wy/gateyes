package provider

import (
	"encoding/json"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestAnthropicRequestHelpersCoverAdditionalBranches(t *testing.T) {
	imageRaw, _ := json.Marshal(map[string]any{"type": "base64", "media_type": "image/png", "data": "abc123"})
	toolRaw := json.RawMessage(`{"city":"Shanghai"}`)

	if got := anthropicBlockToMap(anthropicContentBlock{Type: "thinking", Text: "chain"}); got["text"] != "chain" {
		t.Fatalf("anthropicBlockToMap(thinking) = %+v, want text chain", got)
	}
	if got := anthropicBlockToMap(anthropicContentBlock{Type: "image", Input: imageRaw}); got["type"] != "image" {
		t.Fatalf("anthropicBlockToMap(image) = %+v, want image block", got)
	}
	if got := anthropicBlockToMap(anthropicContentBlock{Type: "tool_use", ID: "tool-1", Name: "lookup", Input: toolRaw}); got["type"] != "tool_use" || got["id"] != "tool-1" {
		t.Fatalf("anthropicBlockToMap(tool_use) = %+v, want tool_use map", got)
	}
	if got := anthropicBlockToMap(anthropicContentBlock{Type: "unknown", Text: "fallback"}); got["text"] != "fallback" {
		t.Fatalf("anthropicBlockToMap(default) = %+v, want fallback text", got)
	}

	if block, ok := buildAnthropicTextBlock(ContentBlock{Type: "thinking", Thinking: "chain"}); !ok || block.Type != "thinking" {
		t.Fatalf("buildAnthropicTextBlock(thinking) = (%+v,%v), want thinking block", block, ok)
	}
	if block, ok := buildAnthropicTextBlock(ContentBlock{Type: "image", Image: &ContentImage{URL: "https://example.com/cat.png"}}); !ok || block.Type != "image" {
		t.Fatalf("buildAnthropicTextBlock(image url) = (%+v,%v), want image block", block, ok)
	}
	if block, ok := buildAnthropicTextBlock(ContentBlock{Type: "refusal", Refusal: "deny"}); !ok || block.Text != "deny" {
		t.Fatalf("buildAnthropicTextBlock(refusal) = (%+v,%v), want text deny", block, ok)
	}
	if block, ok := buildAnthropicTextBlock(ContentBlock{Type: "structured_output", Structured: &StructuredContent{Raw: json.RawMessage(`{"ok":true}`)}}); !ok || block.Text != `{"ok":true}` {
		t.Fatalf("buildAnthropicTextBlock(structured raw) = (%+v,%v), want raw json", block, ok)
	}
	if block, ok := buildAnthropicTextBlock(ContentBlock{Type: "structured_output", Structured: &StructuredContent{Data: map[string]any{"ok": true}}}); !ok || block.Text == "" {
		t.Fatalf("buildAnthropicTextBlock(structured data) = (%+v,%v), want marshaled json", block, ok)
	}
	if _, ok := buildAnthropicTextBlock(ContentBlock{Type: "structured_output", Structured: nil}); ok {
		t.Fatal("buildAnthropicTextBlock(structured nil) ok = true, want false")
	}
	if _, ok := buildAnthropicTextBlock(ContentBlock{Type: "image"}); ok {
		t.Fatal("buildAnthropicTextBlock(image nil) ok = true, want false")
	}
	if _, ok := buildAnthropicTextBlock(ContentBlock{Type: "unknown"}); ok {
		t.Fatal("buildAnthropicTextBlock(unknown) ok = true, want false")
	}

	p := NewAnthropicProvider(config.ProviderConfig{
		Name:      "anthropic-a",
		Type:      "anthropic",
		BaseURL:   "https://anthropic.example",
		APIKey:    "anthropic-key",
		Model:     "claude-test",
		Timeout:   5,
		MaxTokens: 128,
	}).(*anthropicProvider)

	params, err := p.buildParams(&ResponseRequest{
		Model: "claude-public",
		Messages: []Message{
			{Role: "developer", Content: TextBlocks("dev sys")},
			{Role: "user", Content: TextBlocks("hello")},
			{Role: "tool", ToolCallID: "tool-1", Content: TextBlocks("tool output")},
		},
		Tools: []any{
			"skip-me",
			map[string]any{"type": "function"},
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "lookup",
					"description": "lookup weather",
					"parameters":  map[string]any{"type": "object"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildParams(additional) error: %v", err)
	}
	if params["system"] != "dev sys" {
		t.Fatalf("buildParams(additional) system = %#v, want developer prompt promoted to system", params["system"])
	}
	tools, ok := params["tools"].([]map[string]any)
	if !ok || len(tools) != 1 || tools[0]["name"] != "lookup" {
		t.Fatalf("buildParams(additional) tools = %#v, want one normalized tool", params["tools"])
	}

	pNoDefault := NewAnthropicProvider(config.ProviderConfig{
		Name:    "anthropic-b",
		Type:    "anthropic",
		BaseURL: "https://anthropic.example",
		APIKey:  "anthropic-key",
		Model:   "claude-test",
		Timeout: 5,
	}).(*anthropicProvider)
	params, err = pNoDefault.buildParams(&ResponseRequest{
		Model:    "claude-public",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	})
	if err != nil {
		t.Fatalf("buildParams(default tokens) error: %v", err)
	}
	if params["max_tokens"] != 1024 {
		t.Fatalf("buildParams(default tokens) = %#v, want max_tokens 1024", params["max_tokens"])
	}
}
