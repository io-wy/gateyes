package provider

import (
	"encoding/json"
	"testing"
)

func TestMiscProviderBranches(t *testing.T) {
	if normalizeProviderType("") != "openai" || normalizeProviderType(" Anthropic ") != "anthropic" {
		t.Fatalf("normalizeProviderType() returned unexpected values")
	}

	nilManager := (*Manager)(nil)
	nilManager.CloseIdleConnections()
	if got := (&Manager{}).ListByNames(nil); got != nil {
		t.Fatalf("ListByNames(nil) = %+v, want nil", got)
	}

	p := &baseProvider{}
	p.CloseIdleConnections()
	if responsePromptTokens(nil) != 0 {
		t.Fatalf("responsePromptTokens(nil) = %d, want 0", responsePromptTokens(nil))
	}

	if got := ConvertEventToChatChunk("resp-1", "gpt-test", ResponseEvent{Type: EventResponseStarted}); got == nil || got.Object != "chat.completion.chunk" {
		t.Fatalf("ConvertEventToChatChunk(response_started) = %+v, want initial chunk", got)
	}
	if got := ConvertEventToChatChunk("resp-1", "gpt-test", ResponseEvent{
		Type:         EventContentDelta,
		TextDelta:    "hello",
		FinishReason: "stop",
		Usage:        &Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
		ToolCalls: []ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: FunctionCall{
				Name:      "lookup",
				Arguments: "{}",
			},
		}},
	}); got == nil || got.Choices[0].FinishReason != "stop" || got.Usage == nil || len(got.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("ConvertEventToChatChunk(content/tool/usage) = %+v, want merged delta chunk", got)
	}
	if got := ConvertEventToChatChunk("resp-1", "gpt-test", ResponseEvent{Type: EventToolCallDone, Output: nil}); got != nil {
		t.Fatalf("ConvertEventToChatChunk(tool_call_done nil) = %+v, want nil", got)
	}
	if got := ConvertEventToChatChunk("resp-1", "gpt-test", ResponseEvent{Type: EventToolCallDone, Output: &ResponseOutput{Type: "message"}}); got != nil {
		t.Fatalf("ConvertEventToChatChunk(tool_call_done message) = %+v, want nil", got)
	}
	if got := ConvertEventToChatChunk("resp-1", "gpt-test", ResponseEvent{Type: EventResponseCompleted}); got == nil || got.Choices[0].FinishReason != "stop" {
		t.Fatalf("ConvertEventToChatChunk(response_completed nil response) = %+v, want stop chunk", got)
	}

	if ConvertAnthropicRequest(nil) != nil || ConvertResponseToAnthropic(nil) != nil {
		t.Fatal("ConvertAnthropicRequest(nil) or ConvertResponseToAnthropic(nil) returned non-nil")
	}
	if got := convertAnthropicSystem(123); got != "" {
		t.Fatalf("convertAnthropicSystem(non-string) = %q, want empty", got)
	}
	if got := convertAnthropicBlock(AnthropicContentBlock{Type: "image"}); got != nil {
		t.Fatalf("convertAnthropicBlock(image nil source) = %+v, want nil", got)
	}

	if got := buildChatCompletionMessages([]Message{{Role: "user"}}); len(got) != 1 || got[0]["content"] != "" {
		t.Fatalf("buildChatCompletionMessages(empty content) = %+v, want one empty-content message", got)
	}
	if normalizeOpenAITextType("custom") != "custom" {
		t.Fatalf("normalizeOpenAITextType(custom) = %q, want custom", normalizeOpenAITextType("custom"))
	}
	if got := detectResponseFormat([]byte(`{"output":[{}],"choices":[{}]}`)); got != "chat" {
		t.Fatalf("detectResponseFormat(ambiguous with choices) = %q, want chat", got)
	}
	if event, err := parseOpenAIStreamEvent(`{"type":"response.output_item.done","item":{"id":"x","type":"unknown"}}`, "public-model"); err != nil || event != nil {
		t.Fatalf("parseOpenAIStreamEvent(unknown item) = (%+v,%v), want nil,nil", event, err)
	}
	if event, err := parseOpenAIStreamEvent(`{"type":"response.failed","response":{"error":{}}}`, "public-model"); err == nil || event != nil {
		t.Fatalf("parseOpenAIStreamEvent(response.failed empty message) = (%+v,%v), want error", event, err)
	}

	req := &ResponseRequest{
		Messages: []Message{{
			Role: "assistant",
			Content: []ContentBlock{
				{Type: "refusal", Refusal: "deny"},
				{Type: "thinking", Thinking: "chain"},
			},
		}},
	}
	req.Normalize()
	if got := req.OutputFormat; got != nil {
		t.Fatalf("unexpected output format = %+v, want nil", got)
	}
	if text := (&Response{Output: []ResponseOutput{{Type: "message", Content: []ResponseContent{{Type: "refusal", Refusal: "deny"}}}}}).OutputText(); text != "deny" {
		t.Fatalf("Response.OutputText(refusal) = %q, want deny", text)
	}
	if sig := (&Response{Output: []ResponseOutput{{Type: "message", Content: []ResponseContent{{Type: "refusal", Refusal: "deny"}}}}}).Signature(); sig != "deny" {
		t.Fatalf("Response.Signature(refusal) = %q, want deny", sig)
	}
	if (&ResponseRequest{}).HasImageInput() {
		t.Fatal("HasImageInput() on empty request = true, want false")
	}

	if got := normalizeMessages(Message{Role: "user", Content: TextBlocks("hello")}); len(got) != 1 || got[0].Role != "user" {
		t.Fatalf("normalizeMessages(Message) = %+v, want single message", got)
	}
	if got := normalizeMessages(map[string]any{"type": "unknown"}); got != nil {
		t.Fatalf("normalizeMessages(invalid map) = %+v, want nil", got)
	}
	if got := collectText([]ContentBlock{{Type: "thinking", Thinking: "chain"}, {Type: "refusal", Refusal: "deny"}}); got != "chaindeny" {
		t.Fatalf("collectText([]ContentBlock) = %q, want chaindeny", got)
	}
	if got := collectText(map[string]any{"type": "function_call", "text": "ignore"}); got != "" {
		t.Fatalf("collectText(tool-like map) = %q, want empty", got)
	}
	if got := collectText(map[string]any{"content": []any{"a", "b"}}); got != "ab" {
		t.Fatalf("collectText(content map) = %q, want ab", got)
	}

	if got := normalizeContentBlocks([]ContentBlock{{Type: "text", Text: "x"}}); len(got) != 1 || got[0].Text != "x" {
		t.Fatalf("normalizeContentBlocks([]ContentBlock) = %+v, want same content", got)
	}
	if got := normalizeContentBlocks([]any{"a", map[string]any{"type": "text", "text": "b"}}); len(got) != 2 {
		t.Fatalf("normalizeContentBlocks([]any) = %+v, want two blocks", got)
	}
	if got := normalizeContentBlocks(123); len(got) != 1 || got[0].Text != "123" {
		t.Fatalf("normalizeContentBlocks(default) = %+v, want fmt string block", got)
	}

	rawMap := map[string]any{"bad": make(chan int)}
	cloned := cloneStringAnyMapLocal(rawMap)
	if cloned == nil {
		t.Fatal("cloneStringAnyMapLocal(marshal fallback) = nil, want fallback map")
	}
	if _, ok := cloned["bad"]; !ok {
		t.Fatalf("cloneStringAnyMapLocal(marshal fallback) = %+v, want original key retained", cloned)
	}

	rawContent := []ContentBlock{{Type: "structured_output", Structured: &StructuredContent{Raw: json.RawMessage(`{"ok":true}`)}}}
	if clonedContent := cloneContentBlocks(rawContent); len(clonedContent) != 1 || clonedContent[0].Structured == nil {
		t.Fatalf("cloneContentBlocks() = %+v, want cloned structured block", clonedContent)
	}
}
