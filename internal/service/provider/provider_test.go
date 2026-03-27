package provider

import (
	"encoding/json"
	"testing"

	"github.com/gateyes/gateway/internal/config"
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
			Content: []any{
				map[string]any{"type": "text", "text": "hello"},
				map[string]any{"type": "output_text", "text": " world"},
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
			Content:    "{\"ok\":true}",
		},
	}

	items := buildOpenAIInput(messages)
	if len(items) != 3 {
		t.Fatalf("expected 3 input items, got %d", len(items))
	}
	if items[0]["role"] != "user" {
		t.Fatalf("unexpected first role: %#v", items[0]["role"])
	}
	content, ok := items[0]["content"].([]map[string]any)
	if !ok || len(content) != 2 || content[0]["type"] != "input_text" || content[1]["type"] != "input_text" {
		t.Fatalf("unexpected openai content: %#v", items[0]["content"])
	}
	if items[1]["type"] != "function_call" || items[1]["name"] != "lookup_weather" {
		t.Fatalf("unexpected tool call item: %#v", items[1])
	}
	if items[2]["type"] != "function_call_output" || items[2]["call_id"] != "call_1" {
		t.Fatalf("unexpected tool result item: %#v", items[2])
	}
}

func TestConvertOpenAIResponseSupportsFunctionCallOutputs(t *testing.T) {
	raw := openAIResponsePayload{
		ID:        "resp_1",
		CreatedAt: 123,
		Model:     "gpt-test",
		Status:    "completed",
		Output: []openAIOutputItem{
			{
				ID:     "msg_1",
				Type:   "message",
				Role:   "assistant",
				Status: "completed",
				Content: []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}{
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
	}

	resp := convertOpenAIResponse(raw, "")
	if len(resp.Output) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(resp.Output))
	}
	if resp.Output[1].Type != "function_call" || resp.Output[1].Name != "lookup_weather" {
		t.Fatalf("unexpected second output: %+v", resp.Output[1])
	}
}

func TestAnthropicBuildRequestAndConvertResponseSupportToolUse(t *testing.T) {
	p := &anthropicProvider{cfg: config.ProviderConfig{MaxTokens: 256}}
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

	payload, err := p.buildRequest(req, false)
	if err != nil {
		t.Fatalf("buildRequest error: %v", err)
	}
	// 检查基本字段
	if payload["model"] != "claude-test" {
		t.Fatalf("unexpected model: %q", payload["model"])
	}
	messages, ok := payload["messages"].([]map[string]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %#v", payload["messages"])
	}
	if systemParam, _ := payload["system"].(string); systemParam != "sys" {
		t.Fatalf("unexpected system prompt: %#v", payload["system"])
	}

	raw := anthropicResponse{
		ID:    "resp_1",
		Model: "claude-test",
		Role:  "assistant",
		Content: []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		}{
			{Type: "text", Text: "need tool"},
			{Type: "tool_use", ID: "call_1", Name: "lookup_weather", Input: json.RawMessage(`{"city":"shanghai"}`)},
		},
	}

	resp := convertAnthropicResponse(raw, "")
	if len(resp.Output) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(resp.Output))
	}
	if resp.Output[1].Type != "function_call" || resp.Output[1].Args != "{\"city\":\"shanghai\"}" {
		t.Fatalf("unexpected anthropic function call output: %+v", resp.Output[1])
	}
}
