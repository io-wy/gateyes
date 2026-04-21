package provider

import (
	"encoding/json"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestOpenAICompatibilityHelpersCoverBranches(t *testing.T) {
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "thinking", Thinking: "chain"}); !ok || part["type"] != "input_text" || part["text"] != "chain" {
		t.Fatalf("buildOpenAIContentPart(thinking) = (%+v,%v), want input_text chain", part, ok)
	}
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "text", Text: "hello"}); !ok || part["text"] != "hello" {
		t.Fatalf("buildOpenAIContentPart(text) = (%+v,%v), want hello text", part, ok)
	}
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "refusal", Refusal: "deny"}); !ok || part["text"] != "deny" {
		t.Fatalf("buildOpenAIContentPart(refusal) = (%+v,%v), want refusal text", part, ok)
	}
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "image", Image: &ContentImage{URL: "https://example.com/cat.png"}}); !ok || part["image_url"] != "https://example.com/cat.png" {
		t.Fatalf("buildOpenAIContentPart(image_url) = (%+v,%v), want image_url", part, ok)
	}
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "image", Image: &ContentImage{Data: "abc123"}}); !ok || part["image_base64"] != "abc123" {
		t.Fatalf("buildOpenAIContentPart(image_base64) = (%+v,%v), want image_base64", part, ok)
	}
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "structured_output", Structured: &StructuredContent{Data: map[string]any{"ok": true}}}); !ok {
		t.Fatalf("buildOpenAIContentPart(structured_output) = (%+v,%v), want json text", part, ok)
	}
	if _, ok := buildOpenAIContentPart(ContentBlock{Type: "image"}); ok {
		t.Fatal("buildOpenAIContentPart(image nil) ok = true, want false")
	}
	if _, ok := buildOpenAIContentPart(ContentBlock{Type: "unknown"}); ok {
		t.Fatal("buildOpenAIContentPart(unknown) ok = true, want false")
	}

	if got := buildChatCompletionMessages([]Message{{Role: "assistant", ToolCallID: "call-1", ToolCalls: []ToolCall{{ID: "call-1", Type: "function", Function: FunctionCall{Name: "lookup", Arguments: "{}"}}}, Content: TextBlocks("hello")}}); len(got) != 1 {
		t.Fatalf("buildChatCompletionMessages() len = %d, want 1", len(got))
	} else {
		if got[0]["tool_call_id"] != "call-1" || got[0]["tool_calls"] == nil {
			t.Fatalf("buildChatCompletionMessages() = %+v, want tool metadata", got[0])
		}
	}

	imageContent := buildChatCompletionMessageContent([]ContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "image", Image: &ContentImage{URL: "https://example.com/cat.png", Detail: "high"}},
	})
	parts, ok := imageContent.([]map[string]any)
	if !ok || len(parts) != 2 || parts[1]["type"] != "image_url" {
		t.Fatalf("buildChatCompletionMessageContent(image) = %#v, want multipart content", imageContent)
	}
	if part, ok := buildChatCompletionContentPart(ContentBlock{Type: "image", Image: &ContentImage{URL: "https://example.com/cat.png", Detail: "high"}}); !ok || part["type"] != "image_url" {
		t.Fatalf("buildChatCompletionContentPart(image) = (%+v,%v), want image_url", part, ok)
	}
	if _, ok := buildChatCompletionContentPart(ContentBlock{Type: "text"}); ok {
		t.Fatal("buildChatCompletionContentPart(empty text) ok = true, want false")
	}
	if _, ok := buildChatCompletionContentPart(ContentBlock{Type: "unknown"}); ok {
		t.Fatal("buildChatCompletionContentPart(unknown) ok = true, want false")
	}
	if got := normalizeOpenAITextType("output_text"); got != "input_text" {
		t.Fatalf("normalizeOpenAITextType(output_text) = %q, want input_text", got)
	}

	for name, body := range map[string][]byte{
		"responses-by-output": []byte(`{"output":[{}]}`),
		"chat-by-choices":     []byte(`{"choices":[{}]}`),
		"chat-by-object":      []byte(`{"object":"chat.completion.chunk"}`),
		"responses-by-object": []byte(`{"object":"response"}`),
		"unknown":             []byte(`{"id":"x"}`),
	} {
		got := detectResponseFormat(body)
		switch name {
		case "responses-by-output", "responses-by-object":
			if got != "responses" {
				t.Fatalf("detectResponseFormat(%s) = %q, want responses", name, got)
			}
		case "chat-by-choices", "chat-by-object":
			if got != "chat" {
				t.Fatalf("detectResponseFormat(%s) = %q, want chat", name, got)
			}
		case "unknown":
			if got != "unknown" {
				t.Fatalf("detectResponseFormat(%s) = %q, want unknown", name, got)
			}
		}
	}
	if got := detectResponseFormat([]byte(`{`)); got != "unknown" {
		t.Fatalf("detectResponseFormat(invalid json) = %q, want unknown", got)
	}

	if got := convertOpenAIOutputItem(openAIOutputItem{
		Type: "message",
		Content: []struct {
			Type      string `json:"type"`
			Text      string `json:"text"`
			Thinking  string `json:"thinking"`
			Signature string `json:"signature"`
			Refusal   string `json:"refusal"`
		}{
			{Type: "thinking", Thinking: "chain", Signature: "sig-1"},
			{Type: "refusal", Refusal: "deny"},
		},
	}); got == nil || len(got.Content) != 2 || got.Content[0].Type != "thinking" || got.Content[1].Type != "refusal" {
		t.Fatalf("convertOpenAIOutputItem(message branches) = %+v, want thinking/refusal content", got)
	}
	if got := convertOpenAIOutputItem(openAIOutputItem{Type: "unknown"}); got != nil {
		t.Fatalf("convertOpenAIOutputItem(unknown) = %+v, want nil", got)
	}

	raw := chatCompletionResponse{
		ID:      "chat-1",
		Model:   "provider-model",
		Created: 1,
		Choices: []struct {
			Index   int `json:"index"`
			Message struct {
				Role      string     `json:"role"`
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{
			{
				Message: struct {
					Role      string     `json:"role"`
					Content   string     `json:"content"`
					ToolCalls []ToolCall `json:"tool_calls"`
				}{
					Role: "assistant",
					Content: "",
					ToolCalls: []ToolCall{{
						ID:   "call-1",
						Type: "function",
						Function: FunctionCall{Name: "lookup", Arguments: "{}"},
					}},
				},
			},
		},
	}
	converted := convertChatCompletionResponse(raw, "public-model")
	if len(converted.Output) != 1 || converted.Output[0].Type != "function_call" {
		t.Fatalf("convertChatCompletionResponse(tool-only) = %+v, want function_call output", converted.Output)
	}
}

func TestOpenAIParseFallbackResponseBranches(t *testing.T) {
	p := &openAIProvider{}

	respPayload := []byte(`{"id":"resp-1","created_at":1,"model":"provider-model","status":"completed","output":[{"id":"msg-1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`)
	if got, err := p.parseFallbackResponse(respPayload, "public-model"); err != nil || got == nil || got.Model != "public-model" {
		t.Fatalf("parseFallbackResponse(responses) = (%+v,%v), want normalized response", got, err)
	}

	chatPayload := []byte(`{"id":"chat-1","object":"chat.completion","created":1,"model":"provider-model","output":"not-a-responses-array","choices":[{"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)
	if got, err := p.parseFallbackResponse(chatPayload, "public-model"); err != nil || got == nil || got.OutputText() != "hello" {
		t.Fatalf("parseFallbackResponse(chat) = (%+v,%v), want hello", got, err)
	}

	if _, err := p.parseFallbackResponse([]byte(`{"id":`), "public-model"); err == nil {
		t.Fatal("parseFallbackResponse(invalid) error = nil, want parse error")
	}
}

func TestOpenAIClientRequestPathAndPayloadCustomEndpoint(t *testing.T) {
	p := &openAIProvider{baseProvider: newBaseProvider(config.ProviderConfig{
		Endpoint: "/custom/chat",
	})}
	req := &ResponseRequest{Model: "public-model", Messages: []Message{{Role: "user", Content: TextBlocks("hello")}}}
	path, payload := p.requestPathAndPayload(req, false)
	if path != "/custom/chat" {
		t.Fatalf("requestPathAndPayload(custom) path = %q, want /custom/chat", path)
	}
	if payload["messages"] == nil {
		t.Fatalf("requestPathAndPayload(custom) payload = %+v, want chat payload", payload)
	}

	rawSchema := map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "A", "schema": map[string]any{"type": "object"}}}
	built := buildOpenAIChatPayload(&ResponseRequest{
		Model:        "public-model",
		Messages:     []Message{{Role: "user", Content: TextBlocks("hello")}},
		OutputFormat: &OutputFormat{Raw: rawSchema},
	}, true)
	if built["response_format"] == nil || built["stream"] != true {
		t.Fatalf("buildOpenAIChatPayload() = %+v, want response_format + stream", built)
	}

	_ = json.Valid([]byte(`{}`))
}
