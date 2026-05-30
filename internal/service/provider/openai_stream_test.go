package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestOpenAIStreamParsingAdditionalBranches(t *testing.T) {
	if got := detectStreamFormat(`{"type":"response.created"}`); got != "responses" {
		t.Fatalf("detectStreamFormat(response.*) = %q, want responses", got)
	}
	if got := detectStreamFormat(`{"choices":[{"delta":{"content":"hi"}}]}`); got != "chat" {
		t.Fatalf("detectStreamFormat(choices) = %q, want chat", got)
	}

	if event, err := parseSSELine(`{"type":"response.output_text.delta","delta":"hi"}`, "", "public-model"); err != nil || event == nil || event.Text() != "hi" {
		t.Fatalf("parseSSELine(responses delta) = (%+v,%v), want text delta", event, err)
	}

	toolJSON := `{"choices":[{"delta":{"tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}],"finish_reason":"tool_calls"},"index":0}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`
	if event, err := parseSSELine(toolJSON, "chat", "public-model"); err != nil || event == nil || len(event.ToolCalls) != 1 || event.FinishReason != "tool_calls" || event.Usage == nil {
		t.Fatalf("parseSSELine(chat tool calls) = (%+v,%v), want tool_call event with usage", event, err)
	}

	outputItemJSON := `{"type":"response.output_item.done","output_item":{"id":"call-1","type":"function_call","name":"lookup","arguments":"{}"}}`
	if event, err := parseOpenAIStreamEvent(outputItemJSON, "public-model"); err != nil || event == nil || event.Type != EventToolCallDone || event.Output == nil || event.Output.Name != "lookup" {
		t.Fatalf("parseOpenAIStreamEvent(output_item) = (%+v,%v), want tool_call_done", event, err)
	}

	completedJSON := `{"type":"response.completed","response":{"id":"resp-1","created_at":1,"model":"provider-model","status":"completed","output":[{"id":"msg-1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}`
	if event, err := parseOpenAIStreamEvent(completedJSON, "public-model"); err != nil || event == nil || event.Type != EventResponseCompleted || event.Response == nil || event.Response.OutputText() != "done" {
		t.Fatalf("parseOpenAIStreamEvent(response.completed) = (%+v,%v), want completed response", event, err)
	}

	if event, err := parseChatCompletionEvent(`{"choices":[]}`, "public-model"); err != nil || event != nil {
		t.Fatalf("parseChatCompletionEvent(empty choices) = (%+v,%v), want nil,nil", event, err)
	}
}

func TestOpenAIProviderStreamResponseStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer server.Close()

	p := NewOpenAIProvider(config.ProviderConfig{
		Name:    "openai-a",
		Type:    "openai",
		BaseURL: server.URL,
		APIKey:  "test-key",
		Model:   "provider-model",
		Timeout: 5,
	}).(*openAIProvider)

	events, errs := p.StreamResponse(context.Background(), &ResponseRequest{
		Model:    "public-model",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
		Stream:   true,
	})
	for range events {
	}
	var gotErr error
	for err := range errs {
		if err != nil {
			gotErr = err
		}
	}
	if gotErr == nil {
		t.Fatal("StreamResponse(status error) = nil, want upstream error")
	}
}

func TestOpenAIProviderStreamResponseParsesPendingFrameOnDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := NewOpenAIProvider(config.ProviderConfig{
		Name:      "openai-a",
		Type:      "openai",
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "provider-model",
		Timeout:   5,
		Endpoint:  "responses",
		MaxTokens: 64,
	}).(*openAIProvider)

	events, errs := p.StreamResponse(context.Background(), &ResponseRequest{
		Model:    "public-model",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
		Stream:   true,
	})
	var got []ResponseEvent
	for event := range events {
		got = append(got, event)
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("StreamResponse(done flush) error: %v", err)
		}
	}
	if len(got) != 1 || got[0].Text() != "hello" {
		t.Fatalf("StreamResponse(done flush) events = %+v, want pending frame parsed", got)
	}
}

func TestOpenAIProviderStreamResponseParsesPendingFrameOnEOF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"eof hello\"}\n")
	}))
	defer server.Close()

	p := NewOpenAIProvider(config.ProviderConfig{
		Name:      "openai-a",
		Type:      "openai",
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "provider-model",
		Timeout:   5,
		Endpoint:  "responses",
		MaxTokens: 64,
	}).(*openAIProvider)

	events, errs := p.StreamResponse(context.Background(), &ResponseRequest{
		Model:    "public-model",
		Messages: []Message{{Role: "user", Content: TextBlocks("hello")}},
		Stream:   true,
	})
	var got []ResponseEvent
	for event := range events {
		got = append(got, event)
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("StreamResponse(EOF flush) error: %v", err)
		}
	}
	if len(got) != 1 || got[0].Text() != "eof hello" {
		t.Fatalf("StreamResponse(EOF flush) events = %+v, want pending frame parsed on EOF", got)
	}
}
