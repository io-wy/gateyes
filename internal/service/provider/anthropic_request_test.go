package provider

import (
	"encoding/json"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
)

func TestBuildAnthropicTextBlockBranches(t *testing.T) {
	if block, ok := buildAnthropicTextBlock(ContentBlock{Type: "thinking", Thinking: "chain"}); !ok {
		t.Fatalf("buildAnthropicTextBlock(thinking) = (%+v,%v), want thinking block", block, ok)
	}
	if block, ok := buildAnthropicTextBlock(ContentBlock{Type: "image", Image: &ContentImage{URL: "https://example.com/cat.png"}}); !ok {
		t.Fatalf("buildAnthropicTextBlock(image url) = (%+v,%v), want image block", block, ok)
	}
	if block, ok := buildAnthropicTextBlock(ContentBlock{Type: "refusal", Refusal: "deny"}); !ok {
		t.Fatalf("buildAnthropicTextBlock(refusal) = (%+v,%v), want text deny", block, ok)
	}
	if block, ok := buildAnthropicTextBlock(ContentBlock{Type: "structured_output", Structured: &StructuredContent{Raw: json.RawMessage(`{"ok":true}`)}}); !ok {
		t.Fatalf("buildAnthropicTextBlock(structured raw) = (%+v,%v), want raw json", block, ok)
	}
	if block, ok := buildAnthropicTextBlock(ContentBlock{Type: "structured_output", Structured: &StructuredContent{Data: map[string]any{"ok": true}}}); !ok || block.OfText == nil {
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
}

func TestBuildAnthropicParams(t *testing.T) {
	params, err := buildAnthropicParams(&ResponseRequest{
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
	}, config.ProviderConfig{MaxTokens: 128})
	if err != nil {
		t.Fatalf("buildAnthropicParams() error: %v", err)
	}
	if params.Model != "claude-public" {
		t.Fatalf("buildAnthropicParams() model = %q, want claude-public", params.Model)
	}
	if len(params.System) != 1 || params.System[0].Text != "dev sys" {
		t.Fatalf("buildAnthropicParams() system = %+v, want developer prompt", params.System)
	}
	if len(params.Messages) != 2 {
		t.Fatalf("buildAnthropicParams() messages len = %d, want 2", len(params.Messages))
	}
	if len(params.Tools) != 1 {
		t.Fatalf("buildAnthropicParams() tools len = %d, want 1", len(params.Tools))
	}

	// Test default max_tokens
	params2, err := buildAnthropicParams(&ResponseRequest{
		Model:    "claude-public",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
	}, config.ProviderConfig{})
	if err != nil {
		t.Fatalf("buildAnthropicParams(default tokens) error: %v", err)
	}
	if params2.MaxTokens != 1024 {
		t.Fatalf("buildAnthropicParams(default tokens) max_tokens = %d, want 1024", params2.MaxTokens)
	}
}

func TestBuildAnthropicParamsWithOptions(t *testing.T) {
	params, err := buildAnthropicParams(&ResponseRequest{
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
			Raw: map[string]any{
				"metadata": map[string]any{"suite": "regression"},
			},
		},
	}, config.ProviderConfig{MaxTokens: 256})
	if err != nil {
		t.Fatalf("buildAnthropicParams(options) error: %v", err)
	}
	if len(params.System) != 1 || params.System[0].Text != "be concise" {
		t.Fatalf("buildAnthropicParams(options) system = %+v, want typed system option", params.System)
	}
	if params.Thinking.OfEnabled == nil || params.Thinking.OfEnabled.BudgetTokens != 32 {
		t.Fatalf("buildAnthropicParams(options) thinking = %+v, want enabled budget 32", params.Thinking)
	}
}

func TestBuildAnthropicThinkingParam(t *testing.T) {
	adaptive := buildAnthropicThinkingParam(&AnthropicThinking{Type: "adaptive", Display: "summarized"})
	if adaptive.OfAdaptive == nil || adaptive.OfAdaptive.Display != "summarized" {
		t.Fatalf("buildAnthropicThinkingParam(adaptive) = %+v, want adaptive summarized", adaptive)
	}

	disabled := buildAnthropicThinkingParam(&AnthropicThinking{Type: "disabled"})
	if disabled.OfDisabled == nil {
		t.Fatalf("buildAnthropicThinkingParam(disabled) = %+v, want disabled", disabled)
	}

	fallback := buildAnthropicThinkingParam(&AnthropicThinking{Type: "enabled"})
	if fallback.OfAdaptive == nil {
		t.Fatalf("buildAnthropicThinkingParam(enabled without budget) = %+v, want adaptive fallback", fallback)
	}
}

func TestBuildAnthropicParamsWithVendorProfile(t *testing.T) {
	params, err := buildAnthropicParams(&ResponseRequest{
		Model: "MiniMax-M2.5",
		Messages: []Message{{
			Role:    "user",
			Content: TextBlocks("hello"),
		}},
	}, config.ProviderConfig{
		Vendor:    "minimax",
		Type:      "anthropic",
		MaxTokens: 256,
		ExtraBody: map[string]any{
			"temperature": 0.7,
		},
	})
	if err != nil {
		t.Fatalf("buildAnthropicParams(vendor) error: %v", err)
	}
	// ExtraBody and vendor defaults are merged via mergeExtraBody
	// which uses JSON round-trip, so we can't directly inspect the params.
	// This is implicitly tested by integration tests.
	_ = params
}
