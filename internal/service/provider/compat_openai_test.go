package provider

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gateyes/gateway/internal/config"
	"github.com/openai/openai-go"
	oairesponses "github.com/openai/openai-go/responses"
)

func TestChatCompatibilityHelpers(t *testing.T) {
	chatReq := &ChatCompletionRequest{
		Model: "gpt-test",
		Messages: []ChatMessage{{
			Role:    "user",
			Content: "hello",
		}},
		Stream:    true,
		MaxTokens: 99,
	}
	respReq := ConvertChatRequest(chatReq)
	if respReq.Model != "gpt-test" || respReq.Surface != "chat" || !respReq.Stream || respReq.MaxTokens != 99 {
		t.Fatalf("ConvertChatRequest() = %+v, want copied fields", respReq)
	}
	if respReq == nil || respReq.Messages[0].Role != "user" {
		t.Fatalf("ConvertChatRequest() messages = %+v, want cloned messages", respReq.Messages)
	}

	resp := &Response{
		ID:      "resp-1",
		Created: 123,
		Model:   "gpt-test",
		Output:  []ResponseOutput{{Type: "message", Content: []ResponseContent{{Type: "output_text", Text: "hello"}}}},
	}
	chatResp := ConvertResponseToChat(resp)
	if chatResp.Object != "chat.completion" || chatResp.Choices[0].Message.Content != "hello" {
		t.Fatalf("ConvertResponseToChat() = %+v, want chat completion payload", chatResp)
	}
	if ConvertChatRequest(nil) != nil || ConvertResponseToChat(nil) != nil {
		t.Fatal("ConvertChatRequest(nil) or ConvertResponseToChat(nil) returned non-nil")
	}

	chunk := ConvertEventToChatChunk("resp-1", "gpt-test", ResponseEvent{Type: EventContentDelta, Delta: "he"})
	if chunk == nil || chunk.Choices[0].Delta.Content != "he" {
		t.Fatalf("ConvertEventToChatChunk(text) = %+v, want delta content", chunk)
	}
	chunk = ConvertEventToChatChunk("resp-1", "gpt-test", ResponseEvent{
		Type: EventToolCallDone,
		Output: &ResponseOutput{
			ID:   "call-1",
			Type: "function_call",
			Name: "lookup",
			Args: `{"city":"shanghai"}`,
		},
	})
	if chunk == nil || len(chunk.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("ConvertEventToChatChunk(tool) = %+v, want tool call chunk", chunk)
	}
	chunk = ConvertEventToChatChunk("resp-1", "gpt-test", ResponseEvent{
		Type:     EventResponseCompleted,
		Response: &Response{Output: []ResponseOutput{{ID: "call-1", Type: "function_call", Name: "lookup"}}, Usage: Usage{TotalTokens: 5}},
	})
	if chunk == nil || chunk.Choices[0].FinishReason != "tool_calls" || chunk.Usage == nil {
		t.Fatalf("ConvertEventToChatChunk(completed) = %+v, want finish reason and usage", chunk)
	}
	if got := ConvertEventToChatChunk("resp-1", "gpt-test", ResponseEvent{Type: "ignored"}); got == nil {
		t.Fatalf("ConvertEventToChatChunk(ignored) = nil, want empty chunk")
	}
	if RoughTokenCount("") != 0 || RoughTokenCount("12345678") != 2 {
		t.Fatalf("RoughTokenCount() returned unexpected result")
	}
}

func TestOpenAIProviderHelpersAndParsers(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:        "openai-a",
		Type:        "openai",
		BaseURL:     "https://openai.example/",
		APIKey:      "test-key",
		Model:       "provider-model",
		PriceInput:  0.1,
		PriceOutput: 0.2,
		Timeout:     5,
		Endpoint:    "responses",
	}
	p := NewOpenAIProvider(cfg).(*openAIProvider)
	if p.Name() != "openai-a" || p.Type() != "openai" || p.BaseURL() != "https://openai.example/" || p.Model() != "provider-model" {
		t.Fatalf("openAIProvider metadata = (%q,%q,%q,%q), want configured values", p.Name(), p.Type(), p.BaseURL(), p.Model())
	}
	if got, want := p.UnitCost(), 0.30000000000000004; got != want {
		t.Fatalf("openAIProvider.UnitCost() = %v, want %v", got, want)
	}
	if got, want := p.Cost(2, 3), 0.8; got != want {
		t.Fatalf("openAIProvider.Cost() = %v, want %v", got, want)
	}

	// buildChatCompletionMessages now returns SDK types.
	msgs := buildChatCompletionMessages([]Message{{Role: "user", Content: TextBlocks("hello")}})
	if len(msgs) != 1 || msgs[0].OfUser == nil || msgs[0].OfUser.Content.OfString.Value != "hello" {
		t.Fatalf("buildChatCompletionMessages() = %+v, want one simple chat message", msgs)
	}

	// buildOpenAIMessageContent now returns SDK union types.
	parts := buildOpenAIMessageContent([]ContentBlock{{Type: "output_text", Text: "hello"}})
	if len(parts) != 1 || parts[0].OfInputText == nil || parts[0].OfInputText.Text != "hello" {
		t.Fatalf("buildOpenAIMessageContent() = %+v, want normalized input_text block", parts)
	}

	// buildOpenAIContentPart now returns SDK union types.
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "text", Text: "1234"}); !ok || part.OfInputText == nil || part.OfInputText.Text != "1234" {
		t.Fatalf("buildOpenAIContentPart(text block) = (%+v,%v), want text part", part, ok)
	}

	if got := normalizeOpenAITextType("output_text"); got != "input_text" {
		t.Fatalf("normalizeOpenAITextType(output_text) = %q, want %q", got, "input_text")
	}

	// Image content through buildOpenAIMessageContent
	imgParts := buildOpenAIMessageContent([]ContentBlock{{Type: "image", Image: &ContentImage{URL: "https://example.com/cat.png"}}})
	if len(imgParts) != 1 || imgParts[0].OfInputImage == nil {
		t.Fatalf("buildOpenAIMessageContent(image) = %+v, want input_image block", imgParts)
	}

	// parseSDKResponseStreamEvent replaces parseOpenAIResponseEvent.
	var textDelta oairesponses.ResponseStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"response.output_text.delta","delta":"hi","item_id":"item-1","output_index":0,"content_index":0,"sequence_number":1}`), &textDelta)
	delta, err := parseSDKResponseStreamEvent(textDelta, "public-model")
	if err != nil || delta == nil || delta.Delta != "hi" {
		t.Fatalf("parseSDKResponseStreamEvent(delta) = (%+v,%v), want delta hi", delta, err)
	}

	var itemDone oairesponses.ResponseStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"response.output_item.done","item":{"id":"call-1","type":"function_call","name":"lookup","arguments":"{}"},"output_index":0,"sequence_number":2}`), &itemDone)
	itemEvent, err := parseSDKResponseStreamEvent(itemDone, "public-model")
	if err != nil || itemEvent == nil || itemEvent.Output == nil || itemEvent.Output.Name != "lookup" {
		t.Fatalf("parseSDKResponseStreamEvent(item done) = (%+v,%v), want function_call output", itemEvent, err)
	}

	var completed oairesponses.ResponseStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"response.completed","response":{"id":"resp-1","created_at":1,"model":"provider-model","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}},"sequence_number":3}`), &completed)
	completedEvent, err := parseSDKResponseStreamEvent(completed, "public-model")
	if err != nil || completedEvent == nil || completedEvent.Response == nil || completedEvent.Response.Model != "public-model" {
		t.Fatalf("parseSDKResponseStreamEvent(completed) = (%+v,%v), want normalized response model", completedEvent, err)
	}

	var failed oairesponses.ResponseStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"error","message":"boom"}`), &failed)
	if _, err := parseSDKResponseStreamEvent(failed, "public-model"); err == nil {
		t.Fatal("parseSDKResponseStreamEvent(failed) error = nil, want non-nil")
	}

	// Chat completion chunks are handled by parseSDKChatCompletionChunk (tested in openai_stream_test.go).
	// The old parseOpenAIResponseEvent fallback paths (array content, message fallback, text fallback,
	// message tool_calls fallback) are no longer needed because the SDK types enforce a single schema.

	// convertSDKChatCompletion replaces convertChatCompletionResponse.
	resp := openai.ChatCompletion{
		ID:      "chat-1",
		Created: 1,
		Model:   "provider-model",
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{
				Role:    "assistant",
				Content: "hello",
			},
			FinishReason: "stop",
		}},
	}
	converted := convertSDKChatCompletion(resp, "public-model")
	if converted.Model != "public-model" || converted.OutputText() != "hello" || converted.Status != "completed" || converted.Object != "response" {
		t.Fatalf("convertSDKChatCompletion() = %+v, want normalized response", converted)
	}

	format := normalizeOutputFormatValue(map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "Weather",
			"strict": true,
			"schema": map[string]any{"type": "object"},
		},
	})
	if format == nil || format.Type != "json_schema" || format.Name != "Weather" || !format.Strict {
		t.Fatalf("normalizeOutputFormatValue() = %+v, want json_schema format", format)
	}
}

func TestParseSDKChatCompletionChunkHandlesUsageOnly(t *testing.T) {
	// When stream_options: {"include_usage": true}, the last chunk has empty choices but usage.
	chunk := openai.ChatCompletionChunk{
		ID:      "chat-1",
		Object:  "chat.completion.chunk",
		Created: 1,
		Model:   "gpt-4",
		Choices: []openai.ChatCompletionChunkChoice{},
		Usage: openai.CompletionUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}
	event := parseSDKChatCompletionChunk(chunk, "public-model")
	if event == nil || event.Usage == nil || event.Usage.TotalTokens != 15 {
		t.Fatalf("parseSDKChatCompletionChunk(usage-only) = %+v, want usage event", event)
	}
}

func TestConvertOpenAIResponsePreservesRefusalBlock(t *testing.T) {
	resp := convertSDKResponse(oairesponses.Response{
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
					{Type: "refusal", Refusal: "blocked"},
				},
			},
		},
	}, "public-model")

	if len(resp.Output) != 1 || len(resp.Output[0].Content) != 1 {
		t.Fatalf("convertSDKResponse() = %+v, want one refusal message", resp)
	}
	if resp.Output[0].Content[0].Type != "refusal" || resp.Output[0].Content[0].Refusal != "blocked" {
		t.Fatalf("convertSDKResponse() refusal block = %+v, want refusal block", resp.Output[0].Content[0])
	}
}

func TestAnthropicProviderHelpers(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:        "anthropic-a",
		Type:        "anthropic",
		BaseURL:     "https://anthropic.example",
		APIKey:      "anthropic-key",
		Model:       "claude-provider",
		PriceInput:  0.1,
		PriceOutput: 0.2,
		Timeout:     5,
		MaxTokens:   256,
	}
	p := NewAnthropicProvider(cfg).(*anthropicProvider)
	if p.Name() != "anthropic-a" || p.Type() != "anthropic" || p.Model() != "claude-provider" {
		t.Fatalf("anthropicProvider metadata = (%q,%q,%q), want configured values", p.Name(), p.Type(), p.Model())
	}
	if got, want := p.Cost(2, 3), 0.8; got != want {
		t.Fatalf("anthropicProvider.Cost() = %v, want %v", got, want)
	}

	msg := Message{
		Role:    "assistant",
		Content: TextBlocks("hello"),
		ToolCalls: []ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: FunctionCall{
				Name:      "lookup",
				Arguments: `{"city":"shanghai"}`,
			},
		}},
	}
	blocks := buildAnthropicContentBlocks(msg)
	if len(blocks) != 2 || blocks[0].OfText == nil || blocks[1].OfToolUse == nil {
		t.Fatalf("buildAnthropicContentBlocks() = %+v, want text block and tool_use block", blocks)
	}

	if block, ok := buildAnthropicTextBlock(ContentBlock{Type: "text", Text: "123"}); !ok || block.OfText == nil || block.OfText.Text != "123" {
		t.Fatalf("buildAnthropicTextBlock(text block) = (%+v,%v), want text block", block, ok)
	}

	if string(marshalRawJSON(`{"ok":true}`)) != `{"ok":true}` {
		t.Fatalf("marshalRawJSON(valid json) = %q, want %q", string(marshalRawJSON(`{"ok":true}`)), `{"ok":true}`)
	}
	if string(marshalRawJSON(`plain`)) != `"plain"` {
		t.Fatalf("marshalRawJSON(string) = %q, want %q", string(marshalRawJSON(`plain`)), `"plain"`)
	}
	if got := renderOutputSignature([]ResponseOutput{{Type: "message", Content: []ResponseContent{{Text: "hello"}}}, {Type: "function_call", Name: "lookup", Args: "{}"}}); got != "hellolookup{}" {
		t.Fatalf("renderOutputSignature() = %q, want %q", got, "hellolookup{}")
	}

	streamResp := buildAnthropicStreamResponse("resp-1", "claude-public", []ResponseOutput{{Type: "message", Content: []ResponseContent{{Text: "hello"}}}}, 2, 0)
	if streamResp.Usage.TotalTokens <= 2 {
		t.Fatalf("buildAnthropicStreamResponse() = %+v, want computed completion tokens", streamResp.Usage)
	}

	outputs := convertAnthropicOutputs("assistant", []AnthropicContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "tool_use", ID: "call-1", Name: "lookup", Input: json.RawMessage(`{"city":"shanghai"}`)},
	})
	if len(outputs) != 2 || outputs[1].Type != "function_call" {
		t.Fatalf("convertAnthropicOutputs() = %+v, want text and function_call outputs", outputs)
	}
}

func TestConvertAnthropicResponsePreservesThinkingBlock(t *testing.T) {
	resp := convertSDKAnthropicMessage(anthropic.Message{
		ID:    "resp-1",
		Model: "claude-provider",
		Role:  "assistant",
		Content: []anthropic.ContentBlockUnion{
			{Type: "thinking", Thinking: "chain", Signature: "sig-1"},
			{Type: "text", Text: "done"},
		},
		Usage: anthropic.Usage{
			InputTokens:  1,
			OutputTokens: 2,
		},
	}, "claude-public")

	if len(resp.Output) != 1 || len(resp.Output[0].Content) != 2 {
		t.Fatalf("convertSDKAnthropicMessage() = %+v, want one message with thinking + text", resp)
	}
	if resp.Output[0].Content[0].Type != "thinking" || resp.Output[0].Content[0].Thinking != "chain" || resp.Output[0].Content[0].Signature != "sig-1" {
		t.Fatalf("convertSDKAnthropicMessage() thinking block = %+v, want thinking block", resp.Output[0].Content[0])
	}
	if resp.Output[0].Content[1].Type != "output_text" || resp.Output[0].Content[1].Text != "done" {
		t.Fatalf("convertSDKAnthropicMessage() text block = %+v, want output_text block", resp.Output[0].Content[1])
	}
}
