package provider

import (
	"encoding/json"
	"testing"
)

func TestCloneRequestOptionsAndNormalizeContentBranches(t *testing.T) {
	orig := &RequestOptions{
		System: "sys",
		Thinking: &AnthropicThinking{
			Type:         "enabled",
			BudgetTokens: 32,
		},
		CacheControl: &AnthropicCacheControl{
			Type: "ephemeral",
			TTL:  "5m",
		},
		Raw: map[string]any{
			"metadata": map[string]any{"team": "provider"},
		},
	}

	cloned := CloneRequestOptions(orig)
	if cloned == nil || cloned.Thinking == nil || cloned.CacheControl == nil {
		t.Fatalf("CloneRequestOptions() = %+v, want deep cloned options", cloned)
	}
	cloned.Thinking.BudgetTokens = 64
	cloned.CacheControl.TTL = "10m"
	cloned.Raw["metadata"].(map[string]any)["team"] = "mutated"
	if orig.Thinking.BudgetTokens != 32 || orig.CacheControl.TTL != "5m" || orig.Raw["metadata"].(map[string]any)["team"] != "provider" {
		t.Fatalf("CloneRequestOptions() mutated original = %+v", orig)
	}

	blocks := normalizeContentBlocks([]ResponseContent{
		{Type: "thinking", Thinking: "chain", Signature: "sig-1"},
		{Type: "refusal", Text: "deny"},
		{Type: "output_text", Text: "ok"},
	})
	if len(blocks) != 3 || blocks[0].Type != "thinking" || blocks[1].Type != "refusal" || blocks[2].Type != "text" {
		t.Fatalf("normalizeContentBlocks([]ResponseContent) = %+v, want thinking/refusal/text", blocks)
	}

	image := normalizeImageBlock(map[string]any{
		"source": map[string]any{
			"type":       "base64",
			"media_type": "image/png",
			"data":       "abc123",
		},
	})
	if image == nil || image.SourceType != "base64" || image.MediaType != "image/png" || image.Data != "abc123" {
		t.Fatalf("normalizeImageBlock(source) = %+v, want source-based image", image)
	}

	structured := normalizeContentBlocks(map[string]any{
		"type":   "structured_output",
		"format": "json",
		"data":   map[string]any{"ok": true},
	})
	if len(structured) != 1 || structured[0].Structured == nil || structured[0].Structured.Data["ok"] != true {
		t.Fatalf("normalizeContentBlocks(structured_output) = %+v, want structured content", structured)
	}

	if got := responseContentToBlocks(ResponseContent{Type: "refusal", Text: "nope"}); len(got) != 1 || got[0].Refusal != "nope" {
		t.Fatalf("responseContentToBlocks(refusal) = %+v, want refusal block", got)
	}
	if got := responseContentToBlocks(ResponseContent{Type: "output_text", Text: "ok"}); len(got) != 1 || got[0].Text != "ok" {
		t.Fatalf("responseContentToBlocks(output_text) = %+v, want text block", got)
	}
	if got := responseContentToBlocks(ResponseContent{Type: "thinking"}); got != nil {
		t.Fatalf("responseContentToBlocks(empty thinking) = %+v, want nil", got)
	}
	if got := responseContentToBlocks(ResponseContent{Type: "other", Text: "fallback"}); len(got) != 1 || got[0].Text != "fallback" {
		t.Fatalf("responseContentToBlocks(default) = %+v, want fallback text block", got)
	}

	imageURL := normalizeImageBlock(map[string]any{
		"image_url": map[string]any{
			"url":    "https://example.com/cat.png",
			"detail": "high",
		},
	})
	if imageURL == nil || imageURL.URL != "https://example.com/cat.png" || imageURL.Detail != "high" {
		t.Fatalf("normalizeImageBlock(image_url) = %+v, want URL image", imageURL)
	}

	if got := normalizeContentBlocks(map[string]any{"type": "input_text", "input_text": "hello"}); len(got) != 1 || got[0].Text != "hello" {
		t.Fatalf("normalizeContentBlocks(input_text) = %+v, want text block", got)
	}
	if got := normalizeContentBlocks(map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/cat.png"}}); len(got) != 1 || got[0].Type != "image" {
		t.Fatalf("normalizeContentBlocks(image_url map) = %+v, want image block", got)
	}
	if got := normalizeContentBlocks(map[string]any{"type": "refusal", "refusal": ""}); got != nil {
		t.Fatalf("normalizeContentBlocks(empty refusal) = %+v, want nil", got)
	}
	if got := normalizeContentBlocks(map[string]any{"type": "thinking", "thinking": ""}); got != nil {
		t.Fatalf("normalizeContentBlocks(empty thinking) = %+v, want nil", got)
	}
	if got := normalizeContentBlocks(map[string]any{"type": "image"}); got != nil {
		t.Fatalf("normalizeContentBlocks(image without source) = %+v, want nil", got)
	}
	if got := normalizeContentBlocks(map[string]any{"type": "json", "data": map[string]any{"ok": true}}); len(got) != 1 || got[0].Structured == nil {
		t.Fatalf("normalizeContentBlocks(json alias) = %+v, want structured block", got)
	}
	if got := normalizeContentBlocks(map[string]any{"type": "structured_output"}); len(got) != 1 || got[0].Structured == nil {
		t.Fatalf("normalizeContentBlocks(structured without data) = %+v, want empty structured block", got)
	}

	if got := normalizeToolCalls([]any{"bad", map[string]any{"id": "call-1", "type": "function", "function": map[string]any{"name": "lookup", "arguments": "{}"}}}); len(got) != 1 || got[0].Function.Name != "lookup" {
		t.Fatalf("normalizeToolCalls(mixed list) = %+v, want one valid tool call", got)
	}

	deep := cloneStringAnyMapLocal(map[string]any{"nested": map[string]any{"ok": true}})
	deep["nested"].(map[string]any)["ok"] = false
	if deep["nested"].(map[string]any)["ok"] != false {
		t.Fatalf("cloneStringAnyMapLocal(deep copy) = %+v, want mutable cloned nested map", deep)
	}
	if got := cloneStringAnyMapLocal(nil); got != nil {
		t.Fatalf("cloneStringAnyMapLocal(nil) = %+v, want nil", got)
	}
	if got := cloneStringAnyMapLocal(map[string]any{}); got != nil {
		t.Fatalf("cloneStringAnyMapLocal(empty map) = %+v, want nil", got)
	}
	if got := normalizeImageBlock(map[string]any{"type": "image"}); got != nil {
		t.Fatalf("normalizeImageBlock(no image source) = %+v, want nil", got)
	}

	invalidRaw := []ContentBlock{{
		Type:       "structured_output",
		Structured: &StructuredContent{Raw: json.RawMessage(`{`)},
	}}
	if got := cloneContentBlocks(invalidRaw); len(got) != 1 || got[0].Structured == nil {
		t.Fatalf("cloneContentBlocks(invalid raw fallback) = %+v, want original content fallback", got)
	}
}
