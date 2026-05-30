package provider

import (
	"encoding/json"
	"testing"
)

func TestMessageUnmarshalJSONAndRequestFeatureHelpers(t *testing.T) {
	var msg Message
	if err := json.Unmarshal([]byte(`{"role":"user","content":"hello","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}]}`), &msg); err != nil {
		t.Fatalf("Message.UnmarshalJSON() error: %v", err)
	}
	if msg.Role != "user" || len(msg.Content) != 1 || msg.Content[0].Text != "hello" || len(msg.ToolCalls) != 1 {
		t.Fatalf("Message.UnmarshalJSON() = %+v, want normalized content and tool call", msg)
	}

	var nilContent Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":null}`), &nilContent); err != nil {
		t.Fatalf("Message.UnmarshalJSON(null) error: %v", err)
	}
	if nilContent.Content != nil {
		t.Fatalf("Message.UnmarshalJSON(null) content = %+v, want nil", nilContent.Content)
	}

	req := &ResponseRequest{
		Model: "gpt-test",
		Messages: []Message{{
			Role: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "hello"},
				{Type: "image", Image: &ContentImage{URL: "https://example.com/cat.png", Detail: "high"}},
			},
		}},
		Tools:        []any{map[string]any{"type": "function"}},
		OutputFormat: &OutputFormat{Type: "json_schema"},
		MaxTokens:    10,
	}
	if got := req.InputText(); got != "hello" {
		t.Fatalf("ResponseRequest.InputText() = %q, want hello", got)
	}
	if !req.HasToolsRequested() || !req.HasImageInput() || !req.HasStructuredOutputRequest() {
		t.Fatalf("ResponseRequest helpers = tools:%v image:%v structured:%v, want all true", req.HasToolsRequested(), req.HasImageInput(), req.HasStructuredOutputRequest())
	}
	if got := req.EstimateAdmissionTokens(); got <= 10 {
		t.Fatalf("EstimateAdmissionTokens() = %d, want prompt + output budget", got)
	}
}
