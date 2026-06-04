package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestAnthropicStreamStateAndEventBranches(t *testing.T) {
	state := &anthropicStreamState{responseID: "resp-1", model: "claude-test"}
	state.applyContentBlock(AnthropicContentBlock{Type: "text", Text: "hello"})
	state.applyContentBlock(AnthropicContentBlock{Type: "thinking", Thinking: "chain", Signature: "sig-1"})
	state.applyContentBlock(AnthropicContentBlock{Type: "tool_use", ID: "tool-1", Name: "lookup", Input: []byte(`{"city":"Shanghai"}`)})
	if len(state.outputs) != 2 || state.outputs[0].Type != "message" || state.outputs[1].Type != "function_call" {
		t.Fatalf("applyContentBlock() outputs = %+v, want message + function_call", state.outputs)
	}

	// Test message_start event
	var msgStart anthropic.MessageStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"message_start","message":{"id":"resp-2","content":[{"type":"text","text":"prefill"}],"usage":{"input_tokens":2,"cache_read_input_tokens":1}}}`), &msgStart)
	event := handleAnthropicStreamEvent(msgStart, state)
	if event != nil {
		t.Fatalf("handleAnthropicStreamEvent(message_start) = %+v, want nil", event)
	}
	if state.responseID != "resp-2" || state.promptTokens != 2 || state.cachedTokens != 1 {
		t.Fatalf("message_start state = %+v, want response id and usage updated", state)
	}

	// Test content_block_start with thinking
	var thinkingStart anthropic.MessageStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"deep","signature":"sig-2"}}`), &thinkingStart)
	if event := handleAnthropicStreamEvent(thinkingStart, state); event == nil || event.Type != EventThinkingDelta {
		t.Fatalf("handleAnthropicStreamEvent(thinking start) = %+v, want thinking delta", event)
	}

	// Test content_block_start with text
	var textStart anthropic.MessageStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello again"}}`), &textStart)
	if event := handleAnthropicStreamEvent(textStart, state); event == nil || event.Type != EventContentDelta || event.Text() != "hello again" {
		t.Fatalf("handleAnthropicStreamEvent(text start) = %+v, want text delta", event)
	}

	// Test content_block_start with tool_use (opens active tool)
	var toolStart anthropic.MessageStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool-2","name":"lookup","input":{"city":"Beijing"}}}`), &toolStart)
	if event := handleAnthropicStreamEvent(toolStart, state); event != nil {
		t.Fatalf("handleAnthropicStreamEvent(tool_use start) = %+v, want nil while opening tool", event)
	}

	// Test content_block_stop (closes active tool)
	var toolStop anthropic.MessageStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"content_block_stop","index":1}`), &toolStop)
	if event := handleAnthropicStreamEvent(toolStop, state); event == nil || event.Type != EventToolCallDone || event.Output == nil || event.Output.Name != "lookup" {
		t.Fatalf("handleAnthropicStreamEvent(tool_use stop) = %+v, want tool_call_done", event)
	}

	// Test content_block_delta with partial_json
	state.activeTool = &ResponseOutput{Type: "function_call", Args: "{"}
	var partialJSON anthropic.MessageStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"city\":\"Shanghai\"}"}}`), &partialJSON)
	if event := handleAnthropicStreamEvent(partialJSON, state); event != nil {
		t.Fatalf("handleAnthropicStreamEvent(partial_json) = %+v, want nil while appending tool args", event)
	}
	if !strings.Contains(state.activeTool.Args, `"city":"Shanghai"`) {
		t.Fatalf("partial_json active tool args = %q, want appended json fragment", state.activeTool.Args)
	}

	// Test message_delta
	var msgDelta anthropic.MessageStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`), &msgDelta)
	if event := handleAnthropicStreamEvent(msgDelta, state); event != nil {
		t.Fatalf("handleAnthropicStreamEvent(message_delta) = %+v, want nil", event)
	}
	if state.completionTokens != 4 {
		t.Fatalf("message_delta completion tokens = %d, want 4", state.completionTokens)
	}

	// Test message_stop
	var msgStop anthropic.MessageStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"message_stop"}`), &msgStop)
	if event := handleAnthropicStreamEvent(msgStop, state); event == nil || event.Type != EventResponseCompleted {
		t.Fatalf("handleAnthropicStreamEvent(message_stop) = %+v, want completed response", event)
	}

	// Test content_block_stop with no active tool
	var toolStopNoActive anthropic.MessageStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"content_block_stop","index":0}`), &toolStopNoActive)
	if event := handleAnthropicStreamEvent(toolStopNoActive, &anthropicStreamState{}); event != nil {
		t.Fatalf("handleAnthropicStreamEvent(content_block_stop no tool) = %+v, want nil", event)
	}
}

func TestHandleAnthropicStreamEventUnknownType(t *testing.T) {
	state := &anthropicStreamState{responseID: "resp-1", model: "claude-test"}
	var ping anthropic.MessageStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"ping"}`), &ping)
	if event := handleAnthropicStreamEvent(ping, state); event != nil {
		t.Fatalf("handleAnthropicStreamEvent(ping) = %+v, want nil", event)
	}
}
