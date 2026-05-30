package provider

import (
	"strings"
	"testing"
)

func TestNormalizeHelpersAndMessageSignature(t *testing.T) {
	msgs := normalizeMessages([]any{
		"hello",
		map[string]any{
			"role": "assistant",
			"tool_calls": []any{
				map[string]any{
					"id":   "call-1",
					"type": "function",
					"function": map[string]any{
						"name":      "lookup",
						"arguments": `{"city":"shanghai"}`,
					},
				},
			},
		},
		map[string]any{
			"type":    "function_call_output",
			"call_id": "call-1",
			"output":  `{"ok":true}`,
		},
	})
	if len(msgs) != 3 {
		t.Fatalf("normalizeMessages() length = %d, want %d", len(msgs), 3)
	}
	if got := msgs[1].Signature(); !strings.Contains(got, "lookup") {
		t.Fatalf("Message.Signature() = %q, want tool call signature", got)
	}
	if got := collectText([]any{"a", map[string]any{"type": "text", "text": "b"}}); got != "ab" {
		t.Fatalf("collectText() = %q, want %q", got, "ab")
	}
	if got := normalizeContent(map[string]any{"type": "text", "text": "hello"}); got == nil {
		t.Fatal("normalizeContent(map) = nil, want non-nil")
	}
	if got := normalizeContent(""); len(got.([]ContentBlock)) != 0 {
		t.Fatalf("normalizeContent(empty string) = %#v, want empty content blocks", got)
	}
	if got := normalizeToolCalls("bad"); got != nil {
		t.Fatalf("normalizeToolCalls(non-slice) = %+v, want nil", got)
	}
	if !isToolLikeType("function_call") || isToolLikeType("text") {
		t.Fatal("isToolLikeType() returned unexpected result")
	}
	if stringValue(123) != "" || firstNonEmpty("", "b", "c") != "b" {
		t.Fatal("stringValue() or firstNonEmpty() returned unexpected result")
	}

	content := NormalizeMessageContent([]any{
		map[string]any{"type": "thinking", "thinking": "chain"},
		map[string]any{"type": "refusal", "refusal": "denied"},
		map[string]any{"type": "structured_output", "data": map[string]any{"ok": true}},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/cat.png", "detail": "high"}},
	})
	if len(content) != 4 {
		t.Fatalf("NormalizeMessageContent() length = %d, want %d", len(content), 4)
	}
	if content[0].Type != "thinking" || content[0].Thinking != "chain" {
		t.Fatalf("thinking block = %+v, want thinking block", content[0])
	}
	if content[1].Type != "refusal" || content[1].Refusal != "denied" {
		t.Fatalf("refusal block = %+v, want refusal block", content[1])
	}
	if content[2].Type != "structured_output" || content[2].Structured == nil || content[2].Structured.Data["ok"] != true {
		t.Fatalf("structured block = %+v, want structured_output block", content[2])
	}
	if content[3].Type != "image" || content[3].Image == nil || content[3].Image.URL != "https://example.com/cat.png" {
		t.Fatalf("image block = %+v, want image block", content[3])
	}
}
