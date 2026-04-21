package provider

import "testing"

func TestChatStreamEncoderSuppressesDuplicateCompletedChunkAfterFinish(t *testing.T) {
	encoder := NewChatStreamEncoder("resp-1", "gpt-test")

	first := encoder.Encode(ResponseEvent{
		Type:         EventContentDelta,
		FinishReason: "stop",
		Usage:        &Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	})
	if len(first) != 1 {
		t.Fatalf("first finish event emitted %d chunks, want one merged finish chunk", len(first))
	}
	if first[0].Choices[0].Delta.Role != "assistant" {
		t.Fatalf("first chunk role = %q, want assistant", first[0].Choices[0].Delta.Role)
	}
	if first[0].Choices[0].FinishReason != "stop" {
		t.Fatalf("first finish_reason = %q, want stop", first[0].Choices[0].FinishReason)
	}

	second := encoder.Encode(ResponseEvent{
		Type:     EventResponseCompleted,
		Response: &Response{Usage: Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}},
	})
	if len(second) != 0 {
		t.Fatalf("completed after finish emitted %d chunks, want 0", len(second))
	}
}

func TestChatStreamEncoderMergesAssistantRoleIntoFirstToolChunk(t *testing.T) {
	encoder := NewChatStreamEncoder("resp-1", "gpt-test")

	chunks := encoder.Encode(ResponseEvent{
		Type: EventContentDelta,
		ToolCalls: []ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: FunctionCall{
				Name:      "lookup",
				Arguments: `{"city":"Shanghai"}`,
			},
		}},
	})

	if len(chunks) != 1 {
		t.Fatalf("tool-only first event emitted %d chunks, want 1 merged chunk", len(chunks))
	}
	if chunks[0].Choices[0].Delta.Role != "assistant" {
		t.Fatalf("merged tool chunk role = %q, want assistant", chunks[0].Choices[0].Delta.Role)
	}
	if len(chunks[0].Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("merged tool chunk tool_calls = %+v, want one tool call", chunks[0].Choices[0].Delta.ToolCalls)
	}
}

func TestAnthropicStreamEncoderEmitsTextToolAndCompletionLifecycle(t *testing.T) {
	encoder := NewAnthropicStreamEncoder("resp-1", "claude-test")

	started := encoder.Encode(ResponseEvent{
		Type:     EventResponseStarted,
		Response: &Response{Usage: Usage{PromptTokens: 7}},
	})
	if len(started) != 1 || started[0].Type != "message_start" {
		t.Fatalf("response_started emitted %+v, want one message_start", started)
	}
	if started[0].Message == nil || started[0].Message.Usage.InputTokens != 7 {
		t.Fatalf("message_start usage = %+v, want prompt tokens copied", started[0].Message)
	}

	textAndTool := encoder.Encode(ResponseEvent{
		Type:      EventContentDelta,
		TextDelta: "hello",
		ToolCalls: []ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: FunctionCall{
				Name:      "lookup",
				Arguments: `{"city":"shanghai"}`,
			},
		}},
	})
	if len(textAndTool) != 5 {
		t.Fatalf("content_delta emitted %d events, want 5", len(textAndTool))
	}
	if textAndTool[0].Type != "content_block_start" || textAndTool[0].Block == nil || textAndTool[0].Block.Type != "text" {
		t.Fatalf("first event = %+v, want text block start", textAndTool[0])
	}
	if textAndTool[1].Type != "content_block_delta" {
		t.Fatalf("second event = %+v, want text delta", textAndTool[1])
	}
	delta, _ := textAndTool[1].Delta.(map[string]any)
	if delta["text"] != "hello" {
		t.Fatalf("text delta payload = %#v, want hello", textAndTool[1].Delta)
	}
	if textAndTool[2].Type != "content_block_stop" {
		t.Fatalf("third event = %+v, want text block stop before tool", textAndTool[2])
	}
	if textAndTool[3].Type != "content_block_start" || textAndTool[3].Block == nil || textAndTool[3].Block.Type != "tool_use" {
		t.Fatalf("fourth event = %+v, want tool_use block start", textAndTool[3])
	}
	if string(textAndTool[3].Block.Input) != `{"city":"shanghai"}` {
		t.Fatalf("tool input = %s, want canonical json", string(textAndTool[3].Block.Input))
	}
	if textAndTool[4].Type != "content_block_stop" {
		t.Fatalf("fifth event = %+v, want tool block stop", textAndTool[4])
	}

	thinking := encoder.Encode(ResponseEvent{
		Type:          EventThinkingDelta,
		ThinkingDelta: "internal chain",
	})
	if len(thinking) != 2 {
		t.Fatalf("thinking_delta emitted %d events, want 2", len(thinking))
	}
	if thinking[0].Type != "content_block_start" || thinking[0].Block == nil || thinking[0].Block.Type != "thinking" {
		t.Fatalf("thinking first event = %+v, want thinking block start", thinking[0])
	}
	thinkingDelta, _ := thinking[1].Delta.(map[string]any)
	if thinking[1].Type != "content_block_delta" || thinkingDelta["thinking"] != "internal chain" {
		t.Fatalf("thinking delta event = %+v, want thinking delta payload", thinking[1])
	}

	completed := encoder.Encode(ResponseEvent{
		Type: EventResponseCompleted,
		Response: &Response{
			Output: []ResponseOutput{{
				Type:   "function_call",
				CallID: "call-1",
				Name:   "lookup",
				Args:   `{"city":"shanghai"}`,
			}},
			Usage: Usage{
				PromptTokens:     7,
				CompletionTokens: 5,
			},
		},
	})
	if len(completed) != 3 {
		t.Fatalf("response_completed emitted %d events, want 3", len(completed))
	}
	if completed[0].Type != "content_block_stop" {
		t.Fatalf("completion first event = %+v, want active thinking block stop", completed[0])
	}
	if completed[1].Type != "message_delta" {
		t.Fatalf("completion second event = %+v, want message_delta", completed[1])
	}
	stopDelta, _ := completed[1].Delta.(map[string]any)
	if stopDelta["stop_reason"] != "tool_use" {
		t.Fatalf("message_delta payload = %#v, want tool_use", completed[1].Delta)
	}
	if completed[1].Message == nil || completed[1].Message.Usage.OutputTokens != 5 {
		t.Fatalf("message_delta usage = %+v, want completion tokens copied", completed[1].Message)
	}
	if completed[2].Type != "message_stop" {
		t.Fatalf("completion last event = %+v, want message_stop", completed[2])
	}
}

func TestAnthropicStreamEncoderImplicitStartOnCompletedEvent(t *testing.T) {
	encoder := NewAnthropicStreamEncoder("resp-implicit", "claude-test")

	events := encoder.Encode(ResponseEvent{
		Type: EventResponseCompleted,
		Response: &Response{
			Usage: Usage{
				PromptTokens:     3,
				CompletionTokens: 2,
			},
		},
	})
	if len(events) != 3 {
		t.Fatalf("implicit completed emitted %d events, want 3", len(events))
	}
	if events[0].Type != "message_start" || events[0].Message == nil || events[0].Message.Usage.InputTokens != 3 {
		t.Fatalf("implicit start = %+v, want message_start with prompt tokens", events[0])
	}
	if events[1].Type != "message_delta" {
		t.Fatalf("implicit second event = %+v, want message_delta", events[1])
	}
	stopDelta, _ := events[1].Delta.(map[string]any)
	if stopDelta["stop_reason"] != "end_turn" {
		t.Fatalf("implicit message_delta payload = %#v, want end_turn", events[1].Delta)
	}
	if events[2].Type != "message_stop" {
		t.Fatalf("implicit final event = %+v, want message_stop", events[2])
	}
}

func TestAnthropicStreamEncoderAdditionalBranches(t *testing.T) {
	encoder := NewAnthropicStreamEncoder("resp-extra", "claude-test")
	if got := encoder.Encode(ResponseEvent{Type: EventThinkingDelta}); got != nil {
		t.Fatalf("Encode(empty thinking delta) = %+v, want nil", got)
	}

	toolOnly := encoder.Encode(ResponseEvent{
		Type: EventContentDelta,
		ToolCalls: []ToolCall{{
			ID:   "tool-1",
			Type: "function",
			Function: FunctionCall{
				Name:      "lookup",
				Arguments: `{"city":"Shanghai"}`,
			},
		}},
	})
	if len(toolOnly) < 3 || toolOnly[0].Type != "message_start" {
		t.Fatalf("Encode(tool-only delta) = %+v, want message_start + tool_use lifecycle", toolOnly)
	}
	if got := encoder.Encode(ResponseEvent{Type: EventToolCallDone, Output: &ResponseOutput{Type: "message"}}); got != nil {
		t.Fatalf("Encode(tool_call_done non-function) = %+v, want nil", got)
	}
	if got := encoder.Encode(ResponseEvent{Type: "unknown"}); got != nil {
		t.Fatalf("Encode(unknown) = %+v, want nil", got)
	}
}
