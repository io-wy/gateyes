package handler

import (
	"testing"

	"github.com/gateyes/gateway/internal/service/provider"
)

func TestNormalizeResponsesStreamEventRewritesChatDelta(t *testing.T) {
	events := normalizeResponsesStreamEvent(provider.ResponseEvent{
		Type:  provider.EventContentDelta,
		Delta: "hello",
		ToolCalls: []provider.ToolCall{{
			ID: "call-1",
			Function: provider.FunctionCall{
				Name:      "lookup",
				Arguments: `{"city":"Shanghai"}`,
			},
		}},
	})

	if len(events) != 2 {
		t.Fatalf("normalized events = %d, want 2", len(events))
	}
	if events[0].Type != "response.output_text.delta" || events[0].Delta != "hello" {
		t.Fatalf("first normalized event = %+v, want response.output_text.delta", events[0])
	}
	if events[1].Type != "response.output_item.done" || events[1].Output == nil || events[1].Output.Name != "lookup" {
		t.Fatalf("second normalized event = %+v, want response.output_item.done lookup", events[1])
	}
}

func TestNormalizeResponsesStreamEventDropsFinishOnlyChatDelta(t *testing.T) {
	events := normalizeResponsesStreamEvent(provider.ResponseEvent{
		Type:         provider.EventContentDelta,
		FinishReason: "stop",
		Usage:        &provider.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	})
	if len(events) != 0 {
		t.Fatalf("finish-only chat.delta normalized to %d events, want 0", len(events))
	}
}
