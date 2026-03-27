package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestManagerStatsAndFactoryHelpers(t *testing.T) {
	cfgs := []config.ProviderConfig{
		{
			Name:        "openai-a",
			Type:        "openai",
			BaseURL:     "https://openai.example",
			APIKey:      "k1",
			Model:       "gpt-test",
			PriceInput:  0.1,
			PriceOutput: 0.2,
			Timeout:     5,
			Enabled:     true,
		},
		{
			Name:      "anthropic-a",
			Type:      "anthropic",
			BaseURL:   "https://anthropic.example",
			APIKey:    "k2",
			Model:     "claude-test",
			Timeout:   5,
			MaxTokens: 256,
			Enabled:   true,
		},
		{Name: "disabled", Type: "openai", Enabled: false},
	}

	manager, err := NewManager(cfgs)
	if err != nil {
		t.Fatalf("NewManager() error: %v", err)
	}
	if _, ok := manager.Get("openai-a"); !ok {
		t.Fatal("Manager.Get(openai-a) = false, want true")
	}
	if _, ok := manager.Get("disabled"); ok {
		t.Fatal("Manager.Get(disabled) = true, want false")
	}
	if got := len(manager.List()); got != 2 {
		t.Fatalf("len(Manager.List()) = %d, want %d", got, 2)
	}
	if got := len(manager.ListByNames([]string{"anthropic-a", "missing", "openai-a"})); got != 2 {
		t.Fatalf("len(Manager.ListByNames()) = %d, want %d", got, 2)
	}
	if _, err := newProvider(config.ProviderConfig{Name: "bad", Type: "unsupported"}); err == nil {
		t.Fatal("newProvider(unsupported) error = nil, want non-nil")
	}

	stats := NewStats()
	p := NewOpenAIProvider(cfgs[0])
	stats.Register(p)
	stats.RecordRequest("openai-a", true, 10, 20)
	stats.RecordRequest("openai-a", false, 5, 40)
	stats.IncrementLoad("openai-a")
	stats.DecrementLoad("openai-a")

	item, ok := stats.Get("openai-a")
	if !ok {
		t.Fatal("Stats.Get(openai-a) = false, want true")
	}
	if item.TotalRequests != 2 || item.SuccessRequests != 1 || item.FailedRequests != 1 || item.TotalTokens != 15 {
		t.Fatalf("Stats.Get(openai-a) = %+v, want totals 2/1/1/15", item)
	}
	if got := len(stats.List()); got != 1 {
		t.Fatalf("len(Stats.List()) = %d, want %d", got, 1)
	}
	total, success, failed, tokens, avgLatency := stats.GlobalStats()
	if total != 2 || success != 1 || failed != 1 || tokens != 15 || avgLatency != 30 {
		t.Fatalf("Stats.GlobalStats() = (%d,%d,%d,%d,%v), want (2,1,1,15,30)", total, success, failed, tokens, avgLatency)
	}
}

func TestResponseRequestAndResponseHelpers(t *testing.T) {
	req := &ResponseRequest{
		Model:           "gpt-test",
		Input:           "hello world",
		MaxOutputTokens: 42,
	}
	req.Normalize()

	if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		t.Fatalf("ResponseRequest.Normalize() messages = %+v, want one user message", req.Messages)
	}
	if req.RequestedMaxTokens() != 42 {
		t.Fatalf("ResponseRequest.RequestedMaxTokens() = %d, want %d", req.RequestedMaxTokens(), 42)
	}
	if !strings.Contains(req.CacheKey(), "gpt-test") || !strings.Contains(req.CacheKey(), "hello world") {
		t.Fatalf("ResponseRequest.CacheKey() = %q, want model and prompt", req.CacheKey())
	}
	if got := req.EstimatePromptTokens(); got <= 0 {
		t.Fatalf("ResponseRequest.EstimatePromptTokens() = %d, want > 0", got)
	}

	resp := &Response{
		ID:      "resp-1",
		Model:   "gpt-test",
		Created: 123,
		Output: []ResponseOutput{
			{
				Type: "message",
				Content: []ResponseContent{
					{Type: "output_text", Text: "hello"},
					{Type: "output_text", Text: " world"},
				},
			},
			{
				ID:   "call-1",
				Type: "function_call",
				Name: "lookup",
				Args: `{"city":"shanghai"}`,
			},
		},
		Usage: Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5},
	}
	if got, want := resp.OutputText(), "hello world"; got != want {
		t.Fatalf("Response.OutputText() = %q, want %q", got, want)
	}
	if !strings.Contains(resp.Signature(), "lookup") || !strings.Contains(resp.Signature(), "hello") {
		t.Fatalf("Response.Signature() = %q, want text and tool call signature", resp.Signature())
	}
	if got := resp.OutputToolCalls(); len(got) != 1 || got[0].Function.Name != "lookup" {
		t.Fatalf("Response.OutputToolCalls() = %+v, want one lookup call", got)
	}

	textResp := NewTextResponse("resp-2", "gpt-test", "plain text", Usage{TotalTokens: 1})
	if textResp.Status != "completed" || textResp.OutputText() != "plain text" {
		t.Fatalf("NewTextResponse() = %+v, want completed text response", textResp)
	}
}

func TestChatCompatibilityHelpers(t *testing.T) {
	chatReq := &ChatCompletionRequest{
		Model: "gpt-test",
		Messages: []Message{{
			Role:    "user",
			Content: "hello",
		}},
		Stream:    true,
		MaxTokens: 99,
	}
	respReq := ConvertChatRequest(chatReq)
	if respReq.Model != "gpt-test" || !respReq.Stream || respReq.MaxTokens != 99 {
		t.Fatalf("ConvertChatRequest() = %+v, want copied fields", respReq)
	}
	if respReq == nil || respReq.Messages[0].Role != "user" {
		t.Fatalf("ConvertChatRequest() messages = %+v, want cloned messages", respReq.Messages)
	}

	resp := &Response{
		ID:      "resp-1",
		Created: 123,
		Model:   "gpt-test",
		Output: []ResponseOutput{{Type: "message", Content: []ResponseContent{{Type: "output_text", Text: "hello"}}}},
	}
	chatResp := ConvertResponseToChat(resp)
	if chatResp.Object != "chat.completion" || chatResp.Choices[0].Message.Content != "hello" {
		t.Fatalf("ConvertResponseToChat() = %+v, want chat completion payload", chatResp)
	}
	if ConvertChatRequest(nil) != nil || ConvertResponseToChat(nil) != nil {
		t.Fatal("ConvertChatRequest(nil) or ConvertResponseToChat(nil) returned non-nil")
	}

	chunk := ConvertEventToChatChunk("resp-1", "gpt-test", ResponseEvent{Type: "response.output_text.delta", Delta: "he"})
	if chunk == nil || chunk.Choices[0].Delta.Content != "he" {
		t.Fatalf("ConvertEventToChatChunk(text) = %+v, want delta content", chunk)
	}
	chunk = ConvertEventToChatChunk("resp-1", "gpt-test", ResponseEvent{
		Type: "response.output_item.done",
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
		Type:     "response.completed",
		Response: &Response{Output: []ResponseOutput{{ID: "call-1", Type: "function_call", Name: "lookup"}}, Usage: Usage{TotalTokens: 5}},
	})
	if chunk == nil || chunk.Choices[0].FinishReason != "tool_calls" || chunk.Usage == nil {
		t.Fatalf("ConvertEventToChatChunk(completed) = %+v, want finish reason and usage", chunk)
	}
	// 未知事件类型现在返回空 chunk 而非 nil
	if got := ConvertEventToChatChunk("resp-1", "gpt-test", ResponseEvent{Type: "ignored"}); got == nil {
		t.Fatalf("ConvertEventToChatChunk(ignored) = nil, want empty chunk")
	}
	if RoughTokenCount("") != 0 || RoughTokenCount("12345678") != 2 {
		t.Fatalf("RoughTokenCount() returned unexpected result")
	}
}

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
	if got := normalizeContent(""); got != nil {
		t.Fatalf("normalizeContent(empty string) = %#v, want nil", got)
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

	req := &ResponseRequest{Model: "public-model", Input: []Message{{Role: "user", Content: []any{"hello", map[string]any{"type": "text", "text": " world"}}}}, MaxTokens: 12}
	httpReq, err := p.newRequest(context.Background(), req, true)
	if err != nil {
		t.Fatalf("openAIProvider.newRequest(responses) error: %v", err)
	}
	if got, want := httpReq.URL.String(), "https://openai.example/responses"; got != want {
		t.Fatalf("openAIProvider.newRequest(responses) URL = %q, want %q", got, want)
	}
	if got := httpReq.Header.Get("Authorization"); got != "Bearer test-key" {
		t.Fatalf("openAIProvider.newRequest() Authorization = %q, want %q", got, "Bearer test-key")
	}

	p.cfg.Endpoint = "chat"
	httpReq, err = p.newRequest(context.Background(), req, false)
	if err != nil {
		t.Fatalf("openAIProvider.newRequest(chat) error: %v", err)
	}
	if got, want := httpReq.URL.String(), "https://openai.example/v1/chat/completions"; got != want {
		t.Fatalf("openAIProvider.newRequest(chat) URL = %q, want %q", got, want)
	}

	if msgs := buildChatCompletionMessages([]Message{{Role: "user", Content: "hello"}}); len(msgs) != 1 || msgs[0]["content"] != "hello" {
		t.Fatalf("buildChatCompletionMessages() = %+v, want one simple chat message", msgs)
	}
	if parts := buildOpenAIMessageContent([]any{map[string]any{"type": "output_text", "text": "hello"}}); len(parts) != 1 || parts[0]["type"] != "input_text" {
		t.Fatalf("buildOpenAIMessageContent() = %+v, want normalized input_text block", parts)
	}
	if part, ok := buildOpenAIContentPart(1234); !ok || part["text"] != "1234" {
		t.Fatalf("buildOpenAIContentPart(1234) = (%+v,%v), want text part", part, ok)
	}
	if got := normalizeOpenAITextType("output_text"); got != "input_text" {
		t.Fatalf("normalizeOpenAITextType(output_text) = %q, want %q", got, "input_text")
	}

	delta, err := parseOpenAIResponseEvent(`{"type":"response.output_text.delta","delta":"hi"}`, "public-model")
	if err != nil || delta == nil || delta.Delta != "hi" {
		t.Fatalf("parseOpenAIResponseEvent(delta) = (%+v,%v), want delta hi", delta, err)
	}
	itemDone, err := parseOpenAIResponseEvent(`{"type":"response.output_item.done","item":{"id":"call-1","type":"function_call","name":"lookup","arguments":"{}"}}`, "public-model")
	if err != nil || itemDone == nil || itemDone.Output == nil || itemDone.Output.Name != "lookup" {
		t.Fatalf("parseOpenAIResponseEvent(item done) = (%+v,%v), want function_call output", itemDone, err)
	}
	completed, err := parseOpenAIResponseEvent(`{"type":"response.completed","response":{"id":"resp-1","created_at":1,"model":"provider-model","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`, "public-model")
	if err != nil || completed == nil || completed.Response == nil || completed.Response.Model != "public-model" {
		t.Fatalf("parseOpenAIResponseEvent(completed) = (%+v,%v), want normalized response model", completed, err)
	}
	if _, err := parseOpenAIResponseEvent(`{"type":"response.failed","response":{"error":{"message":"boom"}}}`, "public-model"); err == nil {
		t.Fatal("parseOpenAIResponseEvent(failed) error = nil, want non-nil")
	}
	chatEvent, err := parseOpenAIResponseEvent(`{"id":"chat-1","object":"chat.completion.chunk","created":1,"model":"provider-model","choices":[{"delta":{"content":"hello","tool_calls":[{"id":"call-1","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`, "public-model")
	if err != nil || chatEvent == nil || chatEvent.Type != "chat.delta" || len(chatEvent.ToolCalls) != 1 || chatEvent.Usage == nil {
		t.Fatalf("parseOpenAIResponseEvent(chat chunk) = (%+v,%v), want chat.delta with tool calls and usage", chatEvent, err)
	}

	rawResp := chatCompletionResponse{
		ID:      "chat-1",
		Object:  "chat.completion",
		Created: 1,
		Model:   "provider-model",
		Choices: []struct {
			Index        int `json:"index"`
			Message      struct {
				Role       string     `json:"role"`
				Content    string     `json:"content"`
				ToolCalls  []ToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{
			{Message: struct {
				Role       string     `json:"role"`
				Content    string     `json:"content"`
				ToolCalls  []ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "hello"}},
		},
	}
	converted := convertChatCompletionResponse(rawResp, "public-model")
	if converted.Model != "public-model" || converted.OutputText() != "hello" {
		t.Fatalf("convertChatCompletionResponse() = %+v, want normalized response", converted)
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
		Content: []any{map[string]any{"type": "text", "text": "hello"}},
		ToolCalls: []ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: FunctionCall{
				Name:      "lookup",
				Arguments: `{"city":"shanghai"}`,
			},
		}},
	}
	blocks := buildAnthropicBlocks(msg)
	if len(blocks) != 2 || blocks[0].Type != "text" || blocks[1].Type != "tool_use" {
		t.Fatalf("buildAnthropicBlocks() = %+v, want text block and tool_use block", blocks)
	}
	if block, ok := buildAnthropicTextBlock(123); !ok || block.Text != "123" {
		t.Fatalf("buildAnthropicTextBlock(123) = (%+v,%v), want text block", block, ok)
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
