package apicompat

import (
	"testing"

	"github.com/gateyes/gateway/internal/service/provider"
)

func TestChatStreamEncoderSuppressesDuplicateCompletedChunkAfterFinish(t *testing.T) {
	encoder := NewChatStreamEncoder("resp-1", "gpt-test")

	first := encoder.Encode(provider.ResponseEvent{
		Type:         provider.EventContentDelta,
		FinishReason: "stop",
		Usage:        &provider.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
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

	second := encoder.Encode(provider.ResponseEvent{
		Type:     provider.EventResponseCompleted,
		Response: &provider.Response{Usage: provider.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}},
	})
	if len(second) != 0 {
		t.Fatalf("completed after finish emitted %d chunks, want 0", len(second))
	}
}

func TestChatStreamEncoderMergesAssistantRoleIntoFirstToolChunk(t *testing.T) {
	encoder := NewChatStreamEncoder("resp-1", "gpt-test")

	chunks := encoder.Encode(provider.ResponseEvent{
		Type: provider.EventContentDelta,
		ToolCalls: []provider.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: provider.FunctionCall{
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
