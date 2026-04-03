package apicompat

import (
	"testing"

	"github.com/gateyes/gateway/internal/service/provider"
)

func TestChatStreamEncoderSuppressesDuplicateCompletedChunkAfterFinish(t *testing.T) {
	encoder := NewChatStreamEncoder("resp-1", "gpt-test")

	first := encoder.Encode(provider.ResponseEvent{
		Type:         "chat.delta",
		FinishReason: "stop",
		Usage:        &provider.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
	})
	if len(first) != 2 {
		t.Fatalf("first finish event emitted %d chunks, want role + finish", len(first))
	}
	if first[1].Choices[0].FinishReason != "stop" {
		t.Fatalf("first finish_reason = %q, want stop", first[1].Choices[0].FinishReason)
	}

	second := encoder.Encode(provider.ResponseEvent{
		Type:     "response.completed",
		Response: &provider.Response{Usage: provider.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}},
	})
	if len(second) != 0 {
		t.Fatalf("completed after finish emitted %d chunks, want 0", len(second))
	}
}
