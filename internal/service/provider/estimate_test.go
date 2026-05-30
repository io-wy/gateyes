package provider

import (
	"strings"
	"testing"
)

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
