package provider

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gateyes/gateway/internal/app/config"
	oairesponses "github.com/openai/openai-go/responses"
)

func TestNormalizeMessagesSupportsResponseToolItems(t *testing.T) {
	input := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "hello"},
			},
		},
		map[string]any{
			"type":      "function_call",
			"id":        "call_1",
			"name":      "lookup_weather",
			"arguments": "{\"city\":\"shanghai\"}",
		},
		map[string]any{
			"type":    "function_call_output",
			"call_id": "call_1",
			"output":  "{\"ok\":true}",
		},
	}

	messages := normalizeMessages(input)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
	if messages[0].Signature() != "hello" {
		t.Fatalf("unexpected first message signature: %q", messages[0].Signature())
	}
	if len(messages[1].ToolCalls) != 1 || messages[1].ToolCalls[0].Function.Name != "lookup_weather" {
		t.Fatalf("unexpected tool call normalization: %+v", messages[1].ToolCalls)
	}
	if messages[2].ToolCallID != "call_1" || collectText(messages[2].Content) != "{\"ok\":true}" {
		t.Fatalf("unexpected tool result normalization: %+v", messages[2])
	}
}

func TestConvertResponseToChatPreservesToolCalls(t *testing.T) {
	resp := &Response{
		ID:      "resp_1",
		Object:  "response",
		Created: 123,
		Model:   "gpt-test",
		Output: []ResponseOutput{
			{
				Type:   "message",
				Role:   "assistant",
				Status: "completed",
				Content: []ResponseContent{{
					Type: "output_text",
					Text: "need tool",
				}},
			},
			{
				ID:     "call_1",
				Type:   "function_call",
				Status: "completed",
				CallID: "call_1",
				Name:   "lookup_weather",
				Args:   "{\"city\":\"shanghai\"}",
			},
		},
	}

	chat := ConvertResponseToChat(resp)
	if len(chat.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chat.Choices))
	}
	if chat.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %q", chat.Choices[0].FinishReason)
	}
	if chat.Choices[0].Message.Content != "need tool" {
		t.Fatalf("unexpected message content: %#v", chat.Choices[0].Message.Content)
	}
	if len(chat.Choices[0].Message.ToolCalls) != 1 || chat.Choices[0].Message.ToolCalls[0].Function.Name != "lookup_weather" {
		t.Fatalf("unexpected tool calls: %+v", chat.Choices[0].Message.ToolCalls)
	}
}

func TestBuildOpenAIInputSupportsToolCallsAndToolResults(t *testing.T) {
	messages := []Message{
		{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "hello"},
				{Type: "output_text", Text: " world"},
			},
		},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: FunctionCall{
					Name:      "lookup_weather",
					Arguments: "{\"city\":\"shanghai\"}",
				},
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "call_1",
			Content:    TextBlocks("{\"ok\":true}"),
		},
	}

	items := buildOpenAIInput(messages)
	if len(items) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(items))
	}
	// First item: user message
	if items[0].OfMessage == nil {
		t.Fatalf("unexpected first item: %#v", items[0])
	}
	if items[0].OfMessage.Role != "user" {
		t.Fatalf("unexpected first role: %#v", items[0].OfMessage.Role)
	}
	// Second item: function_call
	if items[1].OfFunctionCall == nil || items[1].OfFunctionCall.Name != "lookup_weather" {
		t.Fatalf("unexpected tool call item: %#v", items[1])
	}
	// Third item: function_call_output
	if items[2].OfFunctionCallOutput == nil || items[2].OfFunctionCallOutput.CallID != "call_1" {
		t.Fatalf("unexpected tool result item: %#v", items[2])
	}
}

func TestConvertOpenAIResponseSupportsFunctionCallOutputs(t *testing.T) {
	resp := convertSDKResponse(oairesponses.Response{
		ID:        "resp_1",
		CreatedAt: 123,
		Model:     "gpt-test",
		Status:    "completed",
		Output: []oairesponses.ResponseOutputItemUnion{
			{
				ID:     "msg_1",
				Type:   "message",
				Role:   "assistant",
				Status: "completed",
				Content: []oairesponses.ResponseOutputMessageContentUnion{
					{Type: "output_text", Text: "hello"},
				},
			},
			{
				ID:        "call_1",
				Type:      "function_call",
				Status:    "completed",
				CallID:    "call_1",
				Name:      "lookup_weather",
				Arguments: "{\"city\":\"shanghai\"}",
			},
		},
	}, "")

	if len(resp.Output) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(resp.Output))
	}
	if resp.Output[1].Type != "function_call" || resp.Output[1].Name != "lookup_weather" {
		t.Fatalf("unexpected second output: %+v", resp.Output[1])
	}
}

func TestAnthropicBuildRequestAndConvertResponseSupportToolUse(t *testing.T) {
	req := &ResponseRequest{
		Model: "claude-test",
		Input: []any{
			map[string]any{"role": "system", "content": "sys"},
			map[string]any{"role": "user", "content": "hello"},
			map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "lookup_weather",
							"arguments": "{\"city\":\"shanghai\"}",
						},
					},
				},
			},
			map[string]any{
				"type":    "function_call_output",
				"call_id": "call_1",
				"output":  "{\"ok\":true}",
			},
		},
	}
	req.Normalize()

	// Test request building through buildAnthropicParams
	p := newTestAnthropicProvider(t, config.ProviderConfig{MaxTokens: 256})
	params, err := p.buildAnthropicParams(req)
	if err != nil {
		t.Fatalf("buildAnthropicParams error: %v", err)
	}
	if params.Model != "claude-test" {
		t.Fatalf("unexpected model: %q", params.Model)
	}
	if len(params.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(params.Messages))
	}
	if len(params.System) != 1 || params.System[0].Text != "sys" {
		t.Fatalf("unexpected system prompt: %#v", params.System)
	}

	// Test response conversion through convertSDKAnthropicMessage
	resp := convertSDKAnthropicMessage(anthropic.Message{
		ID:    "resp_1",
		Model: "claude-test",
		Role:  "assistant",
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: "need tool"},
			{Type: "tool_use", ID: "call_1", Name: "lookup_weather", Input: json.RawMessage(`{"city":"shanghai"}`)},
		},
		Usage: anthropic.Usage{
			InputTokens:  1,
			OutputTokens: 2,
		},
	}, "")

	if len(resp.Output) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(resp.Output))
	}
	if resp.Output[1].Type != "function_call" || resp.Output[1].Args != "{\"city\":\"shanghai\"}" {
		t.Fatalf("unexpected anthropic function call output: %+v", resp.Output[1])
	}
}

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

	// buildChatCompletionMessages now returns SDK types; empty messages are skipped.
	msgs := buildChatCompletionMessages([]Message{{Role: "user", Content: TextBlocks("hello")}})
	if len(msgs) != 1 || msgs[0].OfUser == nil {
		t.Fatalf("buildChatCompletionMessages(user) = %+v, want one user message", msgs)
	}
	if normalizeOpenAITextType("custom") != "custom" {
		t.Fatalf("normalizeOpenAITextType(custom) = %q, want custom", normalizeOpenAITextType("custom"))
	}
	if got := detectResponseFormat([]byte(`{"output":[{}],"choices":[{}]}`)); got != "chat" {
		t.Fatalf("detectResponseFormat(ambiguous with choices) = %q, want chat", got)
	}

	// parseSDKResponseStreamEvent replaces parseOpenAIStreamEvent / parseOpenAIResponseEvent.
	var unknownItem oairesponses.ResponseStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"response.output_item.done","item":{"id":"x","type":"unknown"}}`), &unknownItem)
	if event, err := parseSDKResponseStreamEvent(unknownItem, "public-model"); err != nil || event != nil {
		t.Fatalf("parseSDKResponseStreamEvent(unknown item) = (%+v,%v), want nil,nil", event, err)
	}
	var failedEvent oairesponses.ResponseStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"error","message":"boom"}`), &failedEvent)
	if event, err := parseSDKResponseStreamEvent(failedEvent, "public-model"); err == nil || event != nil {
		t.Fatalf("parseSDKResponseStreamEvent(error) = (%+v,%v), want error", event, err)
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
