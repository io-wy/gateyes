package apicompat

import (
	"testing"

	"github.com/gateyes/gateway/internal/service/provider"
)

func TestAnthropicStreamEncoderEmitsTextToolAndCompletionLifecycle(t *testing.T) {
	encoder := NewAnthropicStreamEncoder("resp-1", "claude-test")

	started := encoder.Encode(provider.ResponseEvent{
		Type: provider.EventResponseStarted,
		Response: &provider.Response{
			Usage: provider.Usage{PromptTokens: 7},
		},
	})
	if len(started) != 1 || started[0].Type != "message_start" {
		t.Fatalf("response_started emitted %+v, want one message_start", started)
	}
	if started[0].Message == nil || started[0].Message.Usage.InputTokens != 7 {
		t.Fatalf("message_start usage = %+v, want prompt tokens copied", started[0].Message)
	}

	textAndTool := encoder.Encode(provider.ResponseEvent{
		Type:  provider.EventContentDelta,
		Delta: "hello",
		ToolCalls: []provider.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: provider.FunctionCall{
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

	completed := encoder.Encode(provider.ResponseEvent{
		Type: provider.EventResponseCompleted,
		Response: &provider.Response{
			Output: []provider.ResponseOutput{{
				Type:   "function_call",
				CallID: "call-1",
				Name:   "lookup",
				Args:   `{"city":"shanghai"}`,
			}},
			Usage: provider.Usage{
				PromptTokens:     7,
				CompletionTokens: 5,
			},
		},
	})
	if len(completed) != 2 {
		t.Fatalf("response_completed emitted %d events, want 2", len(completed))
	}
	if completed[0].Type != "message_delta" {
		t.Fatalf("completion first event = %+v, want message_delta", completed[0])
	}
	stopDelta, _ := completed[0].Delta.(map[string]any)
	if stopDelta["stop_reason"] != "tool_use" {
		t.Fatalf("message_delta payload = %#v, want tool_use", completed[0].Delta)
	}
	if completed[0].Message == nil || completed[0].Message.Usage.OutputTokens != 5 {
		t.Fatalf("message_delta usage = %+v, want completion tokens copied", completed[0].Message)
	}
	if completed[1].Type != "message_stop" {
		t.Fatalf("completion last event = %+v, want message_stop", completed[1])
	}
}

func TestAnthropicStreamEncoderImplicitStartOnCompletedEvent(t *testing.T) {
	encoder := NewAnthropicStreamEncoder("resp-implicit", "claude-test")

	events := encoder.Encode(provider.ResponseEvent{
		Type: provider.EventResponseCompleted,
		Response: &provider.Response{
			Usage: provider.Usage{
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
