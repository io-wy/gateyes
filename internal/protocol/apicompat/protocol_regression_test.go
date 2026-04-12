package apicompat

import (
	"testing"

	"github.com/gateyes/gateway/internal/service/provider"
)

func TestConvertChatRequestNormalizesImageInputAndJSONSchema(t *testing.T) {
	req := &ChatCompletionRequest{
		Model: "gpt-test",
		Messages: []provider.ChatMessage{{
			Role: "user",
			Content: []any{
				map[string]any{"type": "text", "text": "look at this"},
				map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url":    "https://example.com/cat.png",
						"detail": "high",
					},
				},
			},
		}},
		ResponseFormat: map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "VisionAnswer",
				"strict": true,
				"schema": map[string]any{"type": "object"},
			},
		},
	}

	converted := ConvertChatRequest(req)
	if converted == nil {
		t.Fatal("ConvertChatRequest() = nil, want response request")
	}
	if len(converted.Messages) != 1 {
		t.Fatalf("ConvertChatRequest() messages = %d, want 1", len(converted.Messages))
	}
	content := converted.Messages[0].Content
	if len(content) != 2 {
		t.Fatalf("ConvertChatRequest() content blocks = %d, want 2", len(content))
	}
	if content[0].Type != "text" || content[0].Text != "look at this" {
		t.Fatalf("ConvertChatRequest() first block = %+v, want text block", content[0])
	}
	if content[1].Type != "image" || content[1].Image == nil || content[1].Image.URL != "https://example.com/cat.png" || content[1].Image.Detail != "high" {
		t.Fatalf("ConvertChatRequest() second block = %+v, want normalized image block", content[1])
	}
	if converted.OutputFormat == nil || converted.OutputFormat.Type != "json_schema" || converted.OutputFormat.Name != "VisionAnswer" || !converted.OutputFormat.Strict {
		t.Fatalf("ConvertChatRequest() output format = %+v, want json_schema output format", converted.OutputFormat)
	}
}

func TestConvertResponseToChatIncludesRefusalText(t *testing.T) {
	resp := &provider.Response{
		ID:      "resp-1",
		Object:  "response",
		Created: 1,
		Model:   "gpt-test",
		Output: []provider.ResponseOutput{{
			Type: "message",
			Role: "assistant",
			Content: []provider.ResponseContent{{
				Type:    "refusal",
				Refusal: "cannot comply",
			}},
		}},
	}

	converted := ConvertResponseToChat(resp)
	if converted == nil || len(converted.Choices) != 1 {
		t.Fatalf("ConvertResponseToChat() = %+v, want one choice", converted)
	}
	if converted.Choices[0].Message.Content != "cannot comply" {
		t.Fatalf("ConvertResponseToChat() content = %#v, want refusal text", converted.Choices[0].Message.Content)
	}
}

func TestConvertResponseToAnthropicPreservesThinkingBlock(t *testing.T) {
	resp := &provider.Response{
		ID:      "resp-1",
		Object:  "response",
		Created: 1,
		Model:   "claude-test",
		Output: []provider.ResponseOutput{{
			Type: "message",
			Role: "assistant",
			Content: []provider.ResponseContent{
				{Type: "thinking", Thinking: "chain", Signature: "sig-1"},
				{Type: "output_text", Text: "done"},
			},
		}},
	}

	converted := ConvertResponseToAnthropic(resp)
	if converted == nil || len(converted.Content) != 2 {
		t.Fatalf("ConvertResponseToAnthropic() = %+v, want two content blocks", converted)
	}
	if converted.Content[0].Type != "thinking" || converted.Content[0].Thinking != "chain" || converted.Content[0].Signature != "sig-1" {
		t.Fatalf("ConvertResponseToAnthropic() first block = %+v, want thinking block", converted.Content[0])
	}
	if converted.Content[1].Type != "text" || converted.Content[1].Text != "done" {
		t.Fatalf("ConvertResponseToAnthropic() second block = %+v, want text block", converted.Content[1])
	}
}

func TestConvertAnthropicRequestNormalizesToolResultBlock(t *testing.T) {
	req := &AnthropicMessagesRequest{
		Model: "claude-test",
		Messages: []AnthropicMessage{
			{
				Role: "assistant",
				Content: []AnthropicContentBlock{{
					Type:  "tool_use",
					ID:    "toolu_1",
					Name:  "get_weather",
					Input: []byte(`{"city":"Shanghai"}`),
				}},
			},
			{
				Role: "user",
				Content: []AnthropicContentBlock{{
					Type:      "tool_result",
					ToolUseID: "toolu_1",
					Content:   "WX-7788: Shanghai is cloudy and 21C.",
				}},
			},
		},
	}

	converted := ConvertAnthropicRequest(req)
	if converted == nil || len(converted.Messages) != 2 {
		t.Fatalf("ConvertAnthropicRequest() = %+v, want two canonical messages", converted)
	}
	if converted.Messages[0].Role != "assistant" || len(converted.Messages[0].ToolCalls) != 1 || converted.Messages[0].ToolCalls[0].Function.Name != "get_weather" {
		t.Fatalf("ConvertAnthropicRequest() first message = %+v, want assistant tool_use", converted.Messages[0])
	}
	if converted.Messages[1].Role != "tool" || converted.Messages[1].ToolCallID != "toolu_1" {
		t.Fatalf("ConvertAnthropicRequest() second message = %+v, want tool result message", converted.Messages[1])
	}
	if text := converted.Messages[1].Content[0].Text; text != "WX-7788: Shanghai is cloudy and 21C." {
		t.Fatalf("ConvertAnthropicRequest() tool result text = %q, want tool result text", text)
	}
}
