package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gateyes/gateway/internal/app/config"
	"github.com/openai/openai-go"
	oairesponses "github.com/openai/openai-go/responses"
)

func TestParseSDKResponseStreamEventHandlesFailure(t *testing.T) {
	var failed oairesponses.ResponseStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"error","message":"upstream exploded"}`), &failed)
	_, err := parseSDKResponseStreamEvent(failed, "public-model")
	if err == nil || err.Error() != "upstream exploded" {
		t.Fatalf("parseSDKResponseStreamEvent(failed) = (_,%v), want upstream exploded error", err)
	}
}

func TestConvertSDKResponseAcceptsNullOutput(t *testing.T) {
	resp := oairesponses.Response{
		ID:        "resp-null-output",
		Object:    "response",
		CreatedAt: 123,
		Model:     "provider-model",
		Status:    "completed",
		Output:    nil,
	}
	converted := convertSDKResponse(resp, "public-model")
	if converted == nil {
		t.Fatal("convertSDKResponse(nil output) = nil, want normalized response")
	}
	if converted.Model != "public-model" || converted.Status != "completed" {
		t.Fatalf("convertSDKResponse(nil output) = %+v, want preserved model/status", converted)
	}
	if len(converted.Output) != 0 {
		t.Fatalf("convertSDKResponse(nil output) output = %+v, want empty slice", converted.Output)
	}
}

func TestConvertSDKChatCompletionHandlesEmptyChoices(t *testing.T) {
	resp := openai.ChatCompletion{
		ID:      "chat-empty",
		Created: 1,
		Model:   "provider-model",
		Choices: []openai.ChatCompletionChoice{},
		Usage:   openai.CompletionUsage{},
	}
	converted := convertSDKChatCompletion(resp, "public-model")
	if converted == nil || len(converted.Output) != 0 {
		t.Fatalf("convertSDKChatCompletion(empty choices) = %+v, want empty output", converted)
	}
}

func TestAnthropicProviderCreateResponseAppliesConfiguredHeaders(t *testing.T) {
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	p := NewAnthropicProvider(config.ProviderConfig{
		Name:      "anthropic-a",
		Type:      "anthropic",
		BaseURL:   server.URL,
		APIKey:    "anthropic-key",
		Model:     "claude-test",
		Timeout:   5,
		MaxTokens: 128,
		Headers: map[string]string{
			"Anthropic-Beta": "tools-2024-04-04",
		},
	}).(*anthropicProvider)

	_, err := p.CreateResponse(context.Background(), &ResponseRequest{
		Model: "claude-test",
		Messages: []Message{{
			Role:    "user",
			Content: TextBlocks("hello"),
		}},
	})
	if err != nil {
		t.Fatalf("CreateResponse() error: %v", err)
	}
	if got := gotHeader.Get("Anthropic-Beta"); got != "tools-2024-04-04" {
		t.Fatalf("Anthropic-Beta header = %q, want configured header", got)
	}
}

func TestAnthropicHandleStreamEventEmitsThinkingDelta(t *testing.T) {
	state := &anthropicStreamState{
		responseID: "resp-1",
		model:      "claude-test",
	}

	var thinkingDelta anthropic.MessageStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"step by step"}}`), &thinkingDelta)
	event := handleAnthropicStreamEvent(thinkingDelta, state)
	if event == nil {
		t.Fatal("handleAnthropicStreamEvent(thinking_delta) = nil, want thinking event")
	}
	if event.Type != EventThinkingDelta || event.ThinkingDelta != "step by step" {
		t.Fatalf("handleAnthropicStreamEvent(thinking_delta) = %+v, want thinking_delta payload", event)
	}
}
