package provider

import (
	"encoding/json"
	"testing"
)

func TestAnthropicMessageUnmarshalJSONSupportsStringAndArray(t *testing.T) {
	var textMsg AnthropicMessage
	if err := json.Unmarshal([]byte(`{"role":"user","content":"hello"}`), &textMsg); err != nil {
		t.Fatalf("AnthropicMessage.UnmarshalJSON(string) error: %v", err)
	}
	if textMsg.Role != "user" || len(textMsg.Content) != 1 || textMsg.Content[0].Type != "text" || textMsg.Content[0].Text != "hello" {
		t.Fatalf("AnthropicMessage.UnmarshalJSON(string) = %+v, want one text block", textMsg)
	}

	var blocksMsg AnthropicMessage
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":[{"type":"text","text":"hi"}]}`), &blocksMsg); err != nil {
		t.Fatalf("AnthropicMessage.UnmarshalJSON(array) error: %v", err)
	}
	if blocksMsg.Role != "assistant" || len(blocksMsg.Content) != 1 || blocksMsg.Content[0].Text != "hi" {
		t.Fatalf("AnthropicMessage.UnmarshalJSON(array) = %+v, want preserved block", blocksMsg)
	}

	var emptyMsg AnthropicMessage
	if err := json.Unmarshal([]byte(`{"role":"assistant"}`), &emptyMsg); err != nil {
		t.Fatalf("AnthropicMessage.UnmarshalJSON(empty) error: %v", err)
	}
	if emptyMsg.Role != "assistant" || emptyMsg.Content != nil {
		t.Fatalf("AnthropicMessage.UnmarshalJSON(empty) = %+v, want role only with nil content", emptyMsg)
	}
}

func TestConvertAnthropicRequestAndResponseCoverCompatibilityBranches(t *testing.T) {
	req := &AnthropicMessagesRequest{
		Model:  "claude-public",
		System: []any{map[string]any{"text": "sys-a"}, map[string]any{"text": "sys-b"}},
		Tools: []AnthropicTool{{
			Name:        "lookup_weather",
			Description: "lookup weather",
			InputSchema: map[string]any{"type": "object"},
		}},
		Thinking: &AnthropicThinking{
			Type:         "enabled",
			BudgetTokens: 64,
		},
		CacheControl: &AnthropicCacheControl{
			Type: "ephemeral",
			TTL:  "5m",
		},
		Messages: []AnthropicMessage{
			{
				Role: "assistant",
				Content: []AnthropicContentBlock{
					{Type: "thinking", Thinking: "internal chain", Signature: "sig-1"},
					{Type: "image", Source: &AnthropicSource{Type: "base64", MediaType: "image/png", Data: "abc123"}},
					{Type: "tool_use", ID: "tool-1", Name: "lookup_weather", Input: json.RawMessage(`{"city":"Shanghai"}`)},
				},
			},
			{
				Role: "user",
				Content: []AnthropicContentBlock{{
					Type:      "tool_result",
					ToolUseID: "tool-1",
					Content:   "Weather is sunny",
				}},
			},
		},
	}

	converted := ConvertAnthropicRequest(req)
	if converted == nil {
		t.Fatal("ConvertAnthropicRequest() = nil, want canonical request")
	}
	if converted.Model != "claude-public" || converted.Surface != "messages" || !converted.Stream == req.Stream {
		t.Fatalf("ConvertAnthropicRequest() = %+v, want preserved model/stream", converted)
	}
	if converted.Options == nil || converted.Options.System != "sys-a\n\nsys-b" {
		t.Fatalf("ConvertAnthropicRequest() system = %+v, want joined system text", converted.Options)
	}
	if converted.Options.Thinking == nil || converted.Options.Thinking.BudgetTokens != 64 {
		t.Fatalf("ConvertAnthropicRequest() thinking = %+v, want typed thinking option", converted.Options)
	}
	if converted.Options.CacheControl == nil || converted.Options.CacheControl.TTL != "5m" {
		t.Fatalf("ConvertAnthropicRequest() cache_control = %+v, want typed cache control", converted.Options)
	}
	if len(converted.Tools) != 1 {
		t.Fatalf("ConvertAnthropicRequest() tools = %+v, want one converted tool", converted.Tools)
	}
	if len(converted.Messages) != 2 {
		t.Fatalf("ConvertAnthropicRequest() messages = %+v, want assistant + tool result", converted.Messages)
	}
	if len(converted.Messages[0].ToolCalls) != 1 || converted.Messages[0].ToolCalls[0].Function.Name != "lookup_weather" {
		t.Fatalf("ConvertAnthropicRequest() first message = %+v, want tool call", converted.Messages[0])
	}
	if len(converted.Messages[0].Content) != 2 || converted.Messages[0].Content[0].Type != "thinking" || converted.Messages[0].Content[1].Type != "image" {
		t.Fatalf("ConvertAnthropicRequest() first message content = %+v, want thinking + image", converted.Messages[0].Content)
	}
	if converted.Messages[1].Role != "tool" || converted.Messages[1].ToolCallID != "tool-1" || converted.Messages[1].Content[0].Text != "Weather is sunny" {
		t.Fatalf("ConvertAnthropicRequest() second message = %+v, want tool result message", converted.Messages[1])
	}

	resp := &Response{
		ID:      "resp-1",
		Object:  "response",
		Created: 1,
		Model:   "claude-public",
		Output: []ResponseOutput{
			{
				Type: "message",
				Role: "assistant",
				Content: []ResponseContent{
					{Type: "thinking", Thinking: "internal chain", Signature: "sig-1"},
					{Type: "output_text", Text: "done"},
				},
			},
			{
				ID:     "tool-1",
				Type:   "function_call",
				CallID: "tool-1",
				Name:   "lookup_weather",
				Args:   `{"city":"Shanghai"}`,
			},
		},
		Usage: Usage{PromptTokens: 3, CompletionTokens: 5},
	}

	convertedResp := ConvertResponseToAnthropic(resp)
	if convertedResp == nil {
		t.Fatal("ConvertResponseToAnthropic() = nil, want anthropic response")
	}
	if convertedResp.StopReason != "tool_use" || convertedResp.Usage.InputTokens != 3 || convertedResp.Usage.OutputTokens != 5 {
		t.Fatalf("ConvertResponseToAnthropic() = %+v, want tool_use stop and mapped usage", convertedResp)
	}
	if len(convertedResp.Content) != 3 {
		t.Fatalf("ConvertResponseToAnthropic() content = %+v, want thinking + text + tool_use", convertedResp.Content)
	}
	if convertedResp.Content[0].Type != "thinking" || convertedResp.Content[1].Type != "text" || convertedResp.Content[2].Type != "tool_use" {
		t.Fatalf("ConvertResponseToAnthropic() content = %+v, want thinking/text/tool_use", convertedResp.Content)
	}

	if got := convertAnthropicSystem("system text"); got != "system text" {
		t.Fatalf("convertAnthropicSystem(string) = %q, want system text", got)
	}
	if got := convertAnthropicBlock(AnthropicContentBlock{Type: "text", Text: "hello"}); len(got) != 1 || got[0].Text != "hello" {
		t.Fatalf("convertAnthropicBlock(text) = %+v, want text block", got)
	}
	if got := convertAnthropicBlock(AnthropicContentBlock{Type: "unknown"}); got != nil {
		t.Fatalf("convertAnthropicBlock(unknown) = %+v, want nil", got)
	}
}
