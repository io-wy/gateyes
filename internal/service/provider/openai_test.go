package provider

import (
	"testing"

	"github.com/openai/openai-go"
	oairesponses "github.com/openai/openai-go/responses"
)

func TestBuildOpenAIContentPartBranches(t *testing.T) {
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "thinking", Thinking: "chain"}); !ok || part.OfInputText == nil || part.OfInputText.Text != "chain" {
		t.Fatalf("buildOpenAIContentPart(thinking) = (%+v,%v), want input_text chain", part, ok)
	}
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "text", Text: "hello"}); !ok || part.OfInputText == nil || part.OfInputText.Text != "hello" {
		t.Fatalf("buildOpenAIContentPart(text) = (%+v,%v), want hello text", part, ok)
	}
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "refusal", Refusal: "deny"}); !ok || part.OfInputText == nil || part.OfInputText.Text != "deny" {
		t.Fatalf("buildOpenAIContentPart(refusal) = (%+v,%v), want denial text", part, ok)
	}
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "image", Image: &ContentImage{URL: "https://example.com/cat.png"}}); !ok || part.OfInputImage == nil {
		t.Fatalf("buildOpenAIContentPart(image_url) = (%+v,%v), want image block", part, ok)
	}
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "image", Image: &ContentImage{Data: "abc123"}}); !ok || part.OfInputImage == nil {
		t.Fatalf("buildOpenAIContentPart(image_base64) = (%+v,%v), want image block", part, ok)
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
}

func TestBuildChatCompletionMessages(t *testing.T) {
	msgs := buildChatCompletionMessages([]Message{{Role: "assistant", ToolCallID: "call-1", ToolCalls: []ToolCall{{ID: "call-1", Type: "function", Function: FunctionCall{Name: "lookup", Arguments: "{}"}}}, Content: TextBlocks("hello")}})
	if len(msgs) != 1 {
		t.Fatalf("buildChatCompletionMessages() len = %d, want 1", len(msgs))
	}
	if msgs[0].OfAssistant == nil {
		t.Fatalf("buildChatCompletionMessages() = %+v, want assistant message", msgs[0])
	}

	imageContent := buildChatCompletionMessages([]Message{{Role: "user", Content: []ContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "image", Image: &ContentImage{URL: "https://example.com/cat.png", Detail: "high"}},
	}}})
	if len(imageContent) != 1 {
		t.Fatalf("buildChatCompletionMessages(image) len = %d, want 1", len(imageContent))
	}

	if part, ok := buildChatCompletionContentPart(ContentBlock{Type: "image", Image: &ContentImage{URL: "https://example.com/cat.png", Detail: "high"}}); !ok || part.GetImageURL() == nil {
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
}

func TestDetectResponseFormat(t *testing.T) {
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
}

func TestConvertSDKOutputItemBranches(t *testing.T) {
	msg := oairesponses.ResponseOutputItemUnion{
		ID:      "msg-1",
		Type:    "message",
		Role:    "assistant",
		Status:  "completed",
		Content: []oairesponses.ResponseOutputMessageContentUnion{},
	}
	got := convertSDKOutputItem(msg)
	if got == nil || got.Type != "message" {
		t.Fatalf("convertSDKOutputItem(message) = %+v, want message", got)
	}

	fn := oairesponses.ResponseOutputItemUnion{
		ID:        "call-1",
		Type:      "function_call",
		CallID:    "call-1",
		Name:      "lookup",
		Arguments: "{}",
	}
	got = convertSDKOutputItem(fn)
	if got == nil || got.Type != "function_call" {
		t.Fatalf("convertSDKOutputItem(function_call) = %+v, want function_call", got)
	}
}

func TestConvertSDKChatCompletion(t *testing.T) {
	resp := openai.ChatCompletion{
		ID:      "chat-1",
		Created: 1,
		Model:   "provider-model",
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{
				Role:    "assistant",
				Content: "",
				ToolCalls: []openai.ChatCompletionMessageToolCall{{
					ID:       "call-1",
					Type:     "function",
					Function: openai.ChatCompletionMessageToolCallFunction{Name: "lookup", Arguments: "{}"},
				}},
			},
			FinishReason: "tool_calls",
		}},
		Usage: openai.CompletionUsage{
			PromptTokens:     1,
			CompletionTokens: 2,
			TotalTokens:      3,
			PromptTokensDetails: openai.CompletionUsagePromptTokensDetails{
				CachedTokens: 1,
			},
		},
	}
	converted := convertSDKChatCompletion(resp, "public-model")
	if len(converted.Output) != 1 || converted.Output[0].Type != "function_call" {
		t.Fatalf("convertSDKChatCompletion(tool-only) = %+v, want function_call output", converted.Output)
	}
	if converted.Usage.CachedTokens != 1 {
		t.Fatalf("convertSDKChatCompletion() cached tokens = %d, want 1", converted.Usage.CachedTokens)
	}
}

func TestConvertSDKResponse(t *testing.T) {
	resp := oairesponses.Response{
		ID:        "resp-1",
		CreatedAt: 1,
		Model:     "provider-model",
		Status:    "completed",
		Output: []oairesponses.ResponseOutputItemUnion{
			{
				ID:     "msg-1",
				Type:   "message",
				Role:   "assistant",
				Status: "completed",
				Content: []oairesponses.ResponseOutputMessageContentUnion{
					{Type: "output_text", Text: "hello"},
				},
			},
		},
		Usage: oairesponses.ResponseUsage{
			InputTokens:  1,
			OutputTokens: 2,
			TotalTokens:  3,
			InputTokensDetails: oairesponses.ResponseUsageInputTokensDetails{
				CachedTokens: 1,
			},
		},
	}
	converted := convertSDKResponse(resp, "public-model")
	if converted.Model != "public-model" || converted.OutputText() != "hello" {
		t.Fatalf("convertSDKResponse() = %+v, want normalized response", converted)
	}
	if converted.Usage.CachedTokens != 1 {
		t.Fatalf("convertSDKResponse() cached tokens = %d, want 1", converted.Usage.CachedTokens)
	}
}
