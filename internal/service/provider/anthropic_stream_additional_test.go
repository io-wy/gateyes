package provider

import (
	"strings"
	"testing"
)

func TestAnthropicStreamStateAndEventBranches(t *testing.T) {
	state := &anthropicStreamState{responseID: "resp-1", model: "claude-test"}
	state.applyContentBlock(AnthropicContentBlock{Type: "text", Text: "hello"})
	state.applyContentBlock(AnthropicContentBlock{Type: "thinking", Thinking: "chain", Signature: "sig-1"})
	state.applyContentBlock(AnthropicContentBlock{Type: "tool_use", ID: "tool-1", Name: "lookup", Input: []byte(`{"city":"Shanghai"}`)})
	if len(state.outputs) != 2 || state.outputs[0].Type != "message" || state.outputs[1].Type != "function_call" {
		t.Fatalf("applyContentBlock() outputs = %+v, want message + function_call", state.outputs)
	}

	start := parseAnthropicStreamEvent("message_start", `{"message":{"id":"resp-2","content":[{"type":"text","text":"prefill"}],"usage":{"input_tokens":2,"cache_hit_input_tokens":1}}}`, state)
	if start != nil {
		t.Fatalf("parseAnthropicStreamEvent(message_start) = %+v, want nil", start)
	}
	if state.responseID != "resp-2" || state.promptTokens != 2 || state.cachedTokens != 1 {
		t.Fatalf("message_start state = %+v, want response id and usage updated", state)
	}

	if event := parseAnthropicStreamEvent("content_block_start", `{"content_block":{"type":"thinking","thinking":"deep","signature":"sig-2"}}`, state); event == nil || event.Type != EventThinkingDelta {
		t.Fatalf("content_block_start(thinking) = %+v, want thinking delta", event)
	}
	if event := parseAnthropicStreamEvent("content_block_start", `{"content_block":{"type":"text","text":"hello again"}}`, state); event == nil || event.Type != EventContentDelta || event.Text() != "hello again" {
		t.Fatalf("content_block_start(text) = %+v, want text delta", event)
	}
	if event := parseAnthropicStreamEvent("content_block_start", `{"content_block":{"type":"tool_use","id":"tool-2","name":"lookup","input":{"city":"Beijing"}}}`, state); event != nil {
		t.Fatalf("content_block_start(tool_use) = %+v, want nil while opening tool", event)
	}
	if event := parseAnthropicStreamEvent("content_block_stop", `{}`, state); event == nil || event.Type != EventToolCallDone || event.Output == nil || event.Output.Name != "lookup" {
		t.Fatalf("content_block_stop(active tool) = %+v, want tool_call_done", event)
	}

	state.activeTool = &ResponseOutput{Type: "function_call", Args: "{"}
	if event := parseAnthropicStreamEvent("content_block_delta", `{"delta":{"partial_json":"\"city\":\"Shanghai\"}"}}`, state); event != nil {
		t.Fatalf("content_block_delta(partial_json) = %+v, want nil while appending tool args", event)
	}
	if !strings.Contains(state.activeTool.Args, `"city":"Shanghai"`) {
		t.Fatalf("partial_json active tool args = %q, want appended json fragment", state.activeTool.Args)
	}

	if event := parseAnthropicStreamEvent("message_delta", `{"content":"done","usage":{"output_tokens":4}}`, state); event == nil || event.Text() != "done" {
		t.Fatalf("message_delta(content) = %+v, want content delta", event)
	}
	if state.completionTokens != 0 {
		t.Fatalf("message_delta(content) completion tokens = %d, want unchanged on content branch", state.completionTokens)
	}
	if event := parseAnthropicStreamEvent("message_delta", `{"usage":{"output_tokens":4}}`, state); event != nil {
		t.Fatalf("message_delta(usage only) = %+v, want nil", event)
	}
	if state.completionTokens != 4 {
		t.Fatalf("message_delta(usage only) completion tokens = %d, want 4", state.completionTokens)
	}
	if event := parseAnthropicStreamEvent("message_delta", `{"delta":{"text":"delta text"}}`, state); event == nil || event.Text() != "delta text" {
		t.Fatalf("message_delta(delta.text) = %+v, want text delta", event)
	}

	if event := parseAnthropicStreamEvent("ping", `{}`, state); event != nil {
		t.Fatalf("ping event = %+v, want nil", event)
	}
	if event := parseAnthropicStreamEvent("", `{"type":"message_stop"}`, state); event == nil || event.Type != EventResponseCompleted {
		t.Fatalf("parseAnthropicStreamEvent(empty eventName) = %+v, want event.Type fallback", event)
	}
	if event := parseAnthropicStreamEvent("text_block", `{"text":"legacy text"}`, state); event == nil || event.Text() != "legacy text" {
		t.Fatalf("text_block event = %+v, want legacy text delta", event)
	}
	if event := parseAnthropicStreamEvent("content_block_stop", `{}`, &anthropicStreamState{}); event != nil {
		t.Fatalf("content_block_stop(no active tool) = %+v, want nil", event)
	}
	if event := parseAnthropicStreamEvent("message_stop", `{}`, state); event == nil || event.Type != EventResponseCompleted {
		t.Fatalf("message_stop event = %+v, want completed response event", event)
	}
}

func TestAnthropicParseStreamErrorAndEOFCompletion(t *testing.T) {
	p := &anthropicProvider{}

	errCh := make(chan error, 1)
	result := make(chan ResponseEvent, 10)
	p.parseStream(strings.NewReader("data: {\"type\":\"error\",\"error\":{\"message\":\"boom\"}}\n\n"), result, errCh, "claude-test")
	select {
	case err := <-errCh:
		if err == nil || err.Error() != "upstream error: 0 boom" {
			t.Fatalf("parseStream(error frame) err = %v, want boom upstream error", err)
		}
	default:
		t.Fatal("parseStream(error frame) err channel empty, want boom")
	}

	errCh = make(chan error, 1)
	result = make(chan ResponseEvent, 10)
	body := "event: message_start\n" +
		"data: {\"message\":{\"id\":\"resp-1\",\"usage\":{\"input_tokens\":2}}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"delta\":{\"text\":\"hello\"}}\n"
	p.parseStream(strings.NewReader(body), result, errCh, "claude-test")
	close(result)
	var events []ResponseEvent
	for event := range result {
		events = append(events, event)
	}
	if len(events) == 0 || events[len(events)-1].Type != EventResponseCompleted {
		t.Fatalf("parseStream(EOF completion) events = %+v, want terminal completed event", events)
	}
}
