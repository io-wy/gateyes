package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/openai/openai-go"
	oairesponses "github.com/openai/openai-go/responses"
)

func TestParseSDKChatCompletionChunk(t *testing.T) {
	chunk := openai.ChatCompletionChunk{
		ID:      "chat-1",
		Object:  "chat.completion.chunk",
		Created: 1,
		Model:   "provider-model",
		Choices: []openai.ChatCompletionChunkChoice{{
			Delta: openai.ChatCompletionChunkChoiceDelta{
				Content: "hello",
			},
			Index:        0,
			FinishReason: "stop",
		}},
	}
	event := parseSDKChatCompletionChunk(chunk, "public-model")
	if event == nil || event.Type != EventContentDelta || event.Text() != "hello" || event.FinishReason != "stop" {
		t.Fatalf("parseSDKChatCompletionChunk() = %+v, want content delta", event)
	}

	// Empty choices should return nil
	emptyChunk := openai.ChatCompletionChunk{Choices: []openai.ChatCompletionChunkChoice{}}
	if event := parseSDKChatCompletionChunk(emptyChunk, "public-model"); event != nil {
		t.Fatalf("parseSDKChatCompletionChunk(empty) = %+v, want nil", event)
	}
}

func TestParseSDKResponseStreamEvent(t *testing.T) {
	// Test response.output_text.delta
	var textDelta oairesponses.ResponseStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"response.output_text.delta","delta":"hello","item_id":"item-1","output_index":0,"content_index":0,"sequence_number":1}`), &textDelta)
	event, err := parseSDKResponseStreamEvent(textDelta, "public-model")
	if err != nil {
		t.Fatalf("parseSDKResponseStreamEvent(text_delta) error: %v", err)
	}
	if event == nil || event.Type != EventContentDelta || event.Text() != "hello" {
		t.Fatalf("parseSDKResponseStreamEvent(text_delta) = %+v, want content delta", event)
	}

	// Test response.output_item.done
	var itemDone oairesponses.ResponseStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"response.output_item.done","item":{"id":"call-1","type":"function_call","name":"lookup","arguments":"{}"},"output_index":0,"sequence_number":2}`), &itemDone)
	event, err = parseSDKResponseStreamEvent(itemDone, "public-model")
	if err != nil {
		t.Fatalf("parseSDKResponseStreamEvent(output_item_done) error: %v", err)
	}
	if event == nil || event.Type != EventToolCallDone || event.Output == nil || event.Output.Name != "lookup" {
		t.Fatalf("parseSDKResponseStreamEvent(output_item_done) = %+v, want tool_call_done", event)
	}

	// Test response.completed
	var completed oairesponses.ResponseStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"response.completed","response":{"id":"resp-1","created_at":1,"model":"provider-model","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}},"sequence_number":3}`), &completed)
	event, err = parseSDKResponseStreamEvent(completed, "public-model")
	if err != nil {
		t.Fatalf("parseSDKResponseStreamEvent(completed) error: %v", err)
	}
	if event == nil || event.Type != EventResponseCompleted {
		t.Fatalf("parseSDKResponseStreamEvent(completed) = %+v, want completed", event)
	}

	// Test response.failed
	var failed oairesponses.ResponseStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"error","message":"upstream exploded"}`), &failed)
	_, err = parseSDKResponseStreamEvent(failed, "public-model")
	if err == nil {
		t.Fatal("parseSDKResponseStreamEvent(failed) = nil, want error")
	}

	// Test unknown event type returns nil
	var unknown oairesponses.ResponseStreamEventUnion
	_ = json.Unmarshal([]byte(`{"type":"response.created","sequence_number":0}`), &unknown)
	event, err = parseSDKResponseStreamEvent(unknown, "public-model")
	if err != nil || event != nil {
		t.Fatalf("parseSDKResponseStreamEvent(unknown) = (%+v,%v), want nil,nil", event, err)
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

func TestOpenAIProviderStreamResponseParsesChatChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"provider-model\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"index\":0}]}\n\n")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"provider-model\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\",\"index\":0}]}\n\n")
	}))
	defer server.Close()

	p := NewOpenAIProvider(config.ProviderConfig{
		Name:      "openai-a",
		Type:      "openai",
		BaseURL:   server.URL,
		APIKey:    "test-key",
		Model:     "provider-model",
		Timeout:   5,
		Endpoint:  "chat",
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
			t.Fatalf("StreamResponse(chat chunks) error: %v", err)
		}
	}
	if len(got) < 1 {
		t.Fatalf("StreamResponse(chat chunks) events = %+v, want at least one event", got)
	}
	var foundText bool
	for _, e := range got {
		if e.Text() == "hello" {
			foundText = true
		}
	}
	if !foundText {
		t.Fatalf("StreamResponse(chat chunks) events = %+v, want hello delta", got)
	}
}
