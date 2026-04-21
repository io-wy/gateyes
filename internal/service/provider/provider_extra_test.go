package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

type closableProviderStub struct {
	closed bool
}

func (p *closableProviderStub) Name() string                                    { return "closable" }
func (p *closableProviderStub) Type() string                                    { return "test" }
func (p *closableProviderStub) BaseURL() string                                 { return "" }
func (p *closableProviderStub) Model() string                                   { return "" }
func (p *closableProviderStub) UnitCost() float64                               { return 0 }
func (p *closableProviderStub) Cost(promptTokens, completionTokens int) float64 { return 0 }
func (p *closableProviderStub) CreateResponse(ctx context.Context, req *ResponseRequest) (*Response, error) {
	return nil, nil
}
func (p *closableProviderStub) StreamResponse(ctx context.Context, req *ResponseRequest) (<-chan ResponseEvent, <-chan error) {
	return nil, nil
}
func (p *closableProviderStub) CloseIdleConnections() {
	p.closed = true
}

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

	manager.CloseIdleConnections()

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

func TestProviderHTTPClientsUseExplicitConnectionPools(t *testing.T) {
	openaiProvider := NewOpenAIProvider(config.ProviderConfig{
		Name:    "openai-a",
		Type:    "openai",
		BaseURL: "https://openai.example",
		APIKey:  "test-key",
		Model:   "gpt-test",
		Timeout: 5,
	}).(*openAIProvider)
	openaiTransport, ok := openaiProvider.client.Transport.(*http.Transport)
	if !ok || openaiTransport == nil {
		t.Fatalf("openai client transport = %T, want *http.Transport", openaiProvider.client.Transport)
	}
	if openaiTransport.MaxIdleConns <= 0 || openaiTransport.MaxIdleConnsPerHost <= 0 || openaiTransport.IdleConnTimeout <= 0 {
		t.Fatalf("openai transport pool = %+v, want explicit positive pool settings", openaiTransport)
	}

	anthropicProvider := NewAnthropicProvider(config.ProviderConfig{
		Name:    "anthropic-a",
		Type:    "anthropic",
		BaseURL: "https://anthropic.example",
		APIKey:  "anthropic-key",
		Model:   "claude-test",
		Timeout: 5,
	}).(*anthropicProvider)
	anthropicTransport, ok := anthropicProvider.client.Transport.(*http.Transport)
	if !ok || anthropicTransport == nil {
		t.Fatalf("anthropic client transport = %T, want *http.Transport", anthropicProvider.client.Transport)
	}
	if anthropicTransport.MaxIdleConns <= 0 || anthropicTransport.MaxIdleConnsPerHost <= 0 || anthropicTransport.IdleConnTimeout <= 0 {
		t.Fatalf("anthropic transport pool = %+v, want explicit positive pool settings", anthropicTransport)
	}
}

func TestManagerCloseIdleConnectionsClosesClosableProviders(t *testing.T) {
	provider := &closableProviderStub{}
	manager := &Manager{
		providers: map[string]Provider{
			"closable": provider,
		},
		Stats: NewStats(),
	}

	manager.CloseIdleConnections()
	if !provider.closed {
		t.Fatal("CloseIdleConnections() did not close idle connections on closable provider")
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

	req := &ResponseRequest{Model: "public-model", Input: []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hello"}, {Type: "text", Text: " world"}}}}, MaxTokens: 12}
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
	p.cfg.BaseURL = "https://openai.example/v1"
	httpReq, err = p.newRequest(context.Background(), req, false)
	if err != nil {
		t.Fatalf("openAIProvider.newRequest(chat with /v1 base) error: %v", err)
	}
	if got, want := httpReq.URL.String(), "https://openai.example/v1/chat/completions"; got != want {
		t.Fatalf("openAIProvider.newRequest(chat with /v1 base) URL = %q, want %q", got, want)
	}

	if msgs := buildChatCompletionMessages([]Message{{Role: "user", Content: TextBlocks("hello")}}); len(msgs) != 1 || msgs[0]["content"] != "hello" {
		t.Fatalf("buildChatCompletionMessages() = %+v, want one simple chat message", msgs)
	}
	if parts := buildOpenAIMessageContent([]ContentBlock{{Type: "output_text", Text: "hello"}}); len(parts) != 1 || parts[0]["type"] != "input_text" {
		t.Fatalf("buildOpenAIMessageContent() = %+v, want normalized input_text block", parts)
	}
	if part, ok := buildOpenAIContentPart(ContentBlock{Type: "text", Text: "1234"}); !ok || part["text"] != "1234" {
		t.Fatalf("buildOpenAIContentPart(text block) = (%+v,%v), want text part", part, ok)
	}
	if got := normalizeOpenAITextType("output_text"); got != "input_text" {
		t.Fatalf("normalizeOpenAITextType(output_text) = %q, want %q", got, "input_text")
	}
	if parts := buildOpenAIMessageContent([]ContentBlock{{Type: "image", Image: &ContentImage{URL: "https://example.com/cat.png"}}}); len(parts) != 1 || parts[0]["type"] != "input_image" {
		t.Fatalf("buildOpenAIMessageContent(image) = %+v, want input_image block", parts)
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
	if err != nil || chatEvent == nil || chatEvent.Type != EventContentDelta || len(chatEvent.ToolCalls) != 1 || chatEvent.Usage == nil {
		t.Fatalf("parseOpenAIResponseEvent(chat chunk) = (%+v,%v), want content_delta with tool calls and usage", chatEvent, err)
	}
	chatEvent, err = parseOpenAIResponseEvent(`{"id":"chat-2","object":"chat.completion.chunk","created":1,"model":"provider-model","choices":[{"delta":{"content":[{"type":"text","text":"hello"},{"type":"text","text":" world"}]}}]}`, "public-model")
	if err != nil || chatEvent == nil || chatEvent.Delta != "hello world" {
		t.Fatalf("parseOpenAIResponseEvent(chat array content) = (%+v,%v), want concatenated delta", chatEvent, err)
	}
	chatEvent, err = parseOpenAIResponseEvent(`{"id":"chat-3","object":"chat.completion.chunk","created":1,"model":"provider-model","choices":[{"message":{"role":"assistant","content":"hello from message"}}]}`, "public-model")
	if err != nil || chatEvent == nil || chatEvent.Delta != "hello from message" {
		t.Fatalf("parseOpenAIResponseEvent(message fallback) = (%+v,%v), want delta from message.content", chatEvent, err)
	}
	chatEvent, err = parseOpenAIResponseEvent(`{"id":"chat-4","object":"chat.completion.chunk","created":1,"model":"provider-model","choices":[{"text":"legacy hello"}]}`, "public-model")
	if err != nil || chatEvent == nil || chatEvent.Delta != "legacy hello" {
		t.Fatalf("parseOpenAIResponseEvent(text fallback) = (%+v,%v), want delta from choice.text", chatEvent, err)
	}
	chatEvent, err = parseOpenAIResponseEvent(`{"id":"chat-5","object":"chat.completion.chunk","created":1,"model":"provider-model","choices":[{"message":{"tool_calls":[{"id":"call-2","type":"function","function":{"name":"lookup","arguments":"{}"}}]}}]}`, "public-model")
	if err != nil || chatEvent == nil || len(chatEvent.ToolCalls) != 1 || chatEvent.ToolCalls[0].Function.Name != "lookup" {
		t.Fatalf("parseOpenAIResponseEvent(message tool_calls fallback) = (%+v,%v), want tool call from message", chatEvent, err)
	}

	rawResp := chatCompletionResponse{
		ID:      "chat-1",
		Object:  "chat.completion",
		Created: 1,
		Model:   "provider-model",
		Choices: []struct {
			Index   int `json:"index"`
			Message struct {
				Role      string     `json:"role"`
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{
			{Message: struct {
				Role      string     `json:"role"`
				Content   string     `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			}{Role: "assistant", Content: "hello"}},
		},
	}
	converted := convertChatCompletionResponse(rawResp, "public-model")
	if converted.Model != "public-model" || converted.OutputText() != "hello" {
		t.Fatalf("convertChatCompletionResponse() = %+v, want normalized response", converted)
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
	blocks := buildAnthropicBlocks(msg)
	if len(blocks) != 2 || blocks[0].Type != "text" || blocks[1].Type != "tool_use" {
		t.Fatalf("buildAnthropicBlocks() = %+v, want text block and tool_use block", blocks)
	}
	if block, ok := buildAnthropicTextBlock(ContentBlock{Type: "text", Text: "123"}); !ok || block.Text != "123" {
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

func TestOpenAIProviderNewRequestEncodesImageInputAndJSONSchema(t *testing.T) {
	formatRaw := map[string]any{
		"type": "json_schema",
		"json_schema": map[string]any{
			"name":   "VisionAnswer",
			"strict": true,
			"schema": map[string]any{"type": "object"},
		},
	}
	req := &ResponseRequest{
		Model: "public-model",
		Messages: []Message{{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "look"},
				{Type: "image", Image: &ContentImage{URL: "https://example.com/cat.png", Detail: "high"}},
			},
		}},
		OutputFormat: &OutputFormat{
			Type:   "json_schema",
			Name:   "VisionAnswer",
			Strict: true,
			Schema: map[string]any{"type": "object"},
			Raw:    formatRaw,
		},
	}
	cfg := config.ProviderConfig{
		Name:     "openai-a",
		Type:     "openai",
		BaseURL:  "https://openai.example",
		APIKey:   "test-key",
		Timeout:  5,
		Endpoint: "chat",
	}
	p := NewOpenAIProvider(cfg).(*openAIProvider)

	t.Run("chat", func(t *testing.T) {
		httpReq, err := p.newRequest(context.Background(), req, false)
		if err != nil {
			t.Fatalf("openAIProvider.newRequest(chat) error: %v", err)
		}
		var payload map[string]any
		body, _ := io.ReadAll(httpReq.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("json.Unmarshal(chat payload) error: %v", err)
		}
		if _, ok := payload["response_format"].(map[string]any); !ok {
			t.Fatalf("chat payload response_format = %#v, want raw response_format", payload["response_format"])
		}
		messages, ok := payload["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("chat payload messages = %#v, want one message", payload["messages"])
		}
		message, _ := messages[0].(map[string]any)
		content, ok := message["content"].([]any)
		if !ok || len(content) != 2 {
			t.Fatalf("chat message content = %#v, want text + image", message["content"])
		}
		imagePart, _ := content[1].(map[string]any)
		if imagePart["type"] != "image_url" {
			t.Fatalf("chat image part type = %#v, want image_url", imagePart["type"])
		}
		imageURL, _ := imagePart["image_url"].(map[string]any)
		if imageURL["url"] != "https://example.com/cat.png" || imageURL["detail"] != "high" {
			t.Fatalf("chat image_url = %#v, want url/detail", imageURL)
		}
	})

	t.Run("responses", func(t *testing.T) {
		p.cfg.Endpoint = "responses"
		httpReq, err := p.newRequest(context.Background(), req, false)
		if err != nil {
			t.Fatalf("openAIProvider.newRequest(responses) error: %v", err)
		}
		var payload map[string]any
		body, _ := io.ReadAll(httpReq.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("json.Unmarshal(responses payload) error: %v", err)
		}
		if _, ok := payload["response_format"].(map[string]any); !ok {
			t.Fatalf("responses payload response_format = %#v, want raw response_format", payload["response_format"])
		}
		input, ok := payload["input"].([]any)
		if !ok || len(input) != 1 {
			t.Fatalf("responses payload input = %#v, want one item", payload["input"])
		}
		item, _ := input[0].(map[string]any)
		content, ok := item["content"].([]any)
		if !ok || len(content) != 2 {
			t.Fatalf("responses input content = %#v, want input_text + input_image", item["content"])
		}
		imagePart, _ := content[1].(map[string]any)
		if imagePart["type"] != "input_image" || imagePart["image_url"] != "https://example.com/cat.png" {
			t.Fatalf("responses image part = %#v, want input_image with URL", imagePart)
		}
	})
}

func TestConvertOpenAIResponsePreservesRefusalBlock(t *testing.T) {
	resp := convertOpenAIResponse(openAIResponsePayload{
		ID:        "resp-1",
		CreatedAt: 1,
		Model:     "provider-model",
		Status:    "completed",
		Output: []openAIOutputItem{{
			ID:     "msg-1",
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []struct {
				Type      string `json:"type"`
				Text      string `json:"text"`
				Thinking  string `json:"thinking"`
				Signature string `json:"signature"`
				Refusal   string `json:"refusal"`
			}{
				{Type: "refusal", Refusal: "blocked"},
			},
		}},
	}, "public-model")

	if len(resp.Output) != 1 || len(resp.Output[0].Content) != 1 {
		t.Fatalf("convertOpenAIResponse() = %+v, want one refusal message", resp)
	}
	if resp.Output[0].Content[0].Type != "refusal" || resp.Output[0].Content[0].Refusal != "blocked" {
		t.Fatalf("convertOpenAIResponse() refusal block = %+v, want refusal block", resp.Output[0].Content[0])
	}
}

func TestConvertAnthropicResponsePreservesThinkingBlock(t *testing.T) {
	resp := convertAnthropicResponse(anthropicResponse{
		ID:    "resp-1",
		Model: "claude-provider",
		Role:  "assistant",
		Content: []struct {
			Type      string           `json:"type"`
			Text      string           `json:"text"`
			ID        string           `json:"id"`
			Name      string           `json:"name"`
			Input     json.RawMessage  `json:"input"`
			Source    *AnthropicSource `json:"source"`
			Thinking  string           `json:"thinking"`
			Signature string           `json:"signature"`
		}{
			{Type: "thinking", Thinking: "chain", Signature: "sig-1"},
			{Type: "text", Text: "done"},
		},
	}, "claude-public")

	if len(resp.Output) != 1 || len(resp.Output[0].Content) != 2 {
		t.Fatalf("convertAnthropicResponse() = %+v, want one message with thinking + text", resp)
	}
	if resp.Output[0].Content[0].Type != "thinking" || resp.Output[0].Content[0].Thinking != "chain" || resp.Output[0].Content[0].Signature != "sig-1" {
		t.Fatalf("convertAnthropicResponse() thinking block = %+v, want thinking block", resp.Output[0].Content[0])
	}
	if resp.Output[0].Content[1].Type != "output_text" || resp.Output[0].Content[1].Text != "done" {
		t.Fatalf("convertAnthropicResponse() text block = %+v, want output_text block", resp.Output[0].Content[1])
	}
}
