package responses

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestCreate_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-ok","object":"chat.completion","created":1700000000,"model":"provider-model","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`))
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	result, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "session-1")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if result.ProviderName != "test-openai" {
		t.Fatalf("Create() provider = %q, want test-openai", result.ProviderName)
	}
	if result.Response == nil || result.Response.OutputText() != "hello" {
		t.Fatalf("Create() response text unexpected")
	}
}

func TestCreate_ModelUnavailable(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: "http://127.0.0.1:1",
		providers:   []string{"test-openai"},
		providerConfigs: []config.ProviderConfig{{
			Name:      "test-openai",
			Type:      "openai",
			BaseURL:   "http://127.0.0.1:1",
			Endpoint:  "chat",
			APIKey:    "k",
			Model:     "provider-model",
			Timeout:   1,
			Enabled:   true,
			MaxTokens: 256,
		}},
	})
	env.providerMgr.ApplyRegistry([]repository.ProviderRegistryRecord{{
		Name:         "test-openai",
		Enabled:      true,
		Drain:        true,
		HealthStatus: provider.ProviderHealthHealthy,
	}})

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "provider-model",
		Input: "hello",
	}, "")
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("Create() error = %v, want %v", err, ErrNoProvider)
	}
}

func TestCreate_RetryExhausted(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusBadGateway)
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if err == nil {
		t.Fatal("Create() expected error after retry exhausted")
	}
}

func TestCreate_ProviderNotFound(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: "http://127.0.0.1:1",
		providers:   []string{},
	})

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
	}, "")
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("Create() error = %v, want %v", err, ErrNoProvider)
	}
}

func TestCreateStream_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"stream ok\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_up\",\"created_at\":1700000000,\"model\":\"provider-model\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"stream ok\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":2,\"total_tokens\":5}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "responses",
		providers:   []string{"test-openai"},
	})

	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input:  "hello",
		Stream: true,
	}, "session-stream")
	if err != nil {
		t.Fatalf("CreateStream() error: %v", err)
	}

	var eventTypes []string
	for e := range stream.Events {
		eventTypes = append(eventTypes, e.Type)
	}
	if len(eventTypes) < 2 || eventTypes[0] != provider.EventResponseStarted || eventTypes[len(eventTypes)-1] != provider.EventResponseCompleted {
		t.Fatalf("unexpected events: %v", eventTypes)
	}
}

func TestCreateStream_RetryFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"fallback\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_fb\",\"created_at\":1700000000,\"model\":\"provider-model\",\"status\":\"completed\",\"output\":[{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"fallback\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":2,\"total_tokens\":5}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "responses",
		providers:   []string{"fail-openai", "test-openai"},
		providerConfigs: []config.ProviderConfig{
			{Name: "fail-openai", Type: "openai", BaseURL: "http://127.0.0.1:1", Endpoint: "responses", APIKey: "k", Model: "m1", Timeout: 1, Enabled: true, MaxTokens: 256},
			{Name: "test-openai", Type: "openai", BaseURL: upstream.URL, Endpoint: "responses", APIKey: "k", Model: "provider-model", Timeout: 5, Enabled: true, MaxTokens: 256},
		},
	})

	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input:  "hello",
		Stream: true,
	}, "session-fb")
	if err != nil {
		t.Fatalf("CreateStream() error: %v", err)
	}

	var deltas []string
	var streamErr error
	for eventsCh := stream.Events; eventsCh != nil; {
		select {
		case e, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				continue
			}
			if e.Delta != "" {
				deltas = append(deltas, e.Delta)
			}
		case err := <-stream.Errors:
			if err != nil {
				streamErr = err
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out")
		}
	}
	if streamErr != nil {
		t.Fatalf("unexpected stream error: %v", streamErr)
	}
	if len(deltas) == 0 || deltas[0] != "fallback" {
		t.Fatalf("unexpected deltas: %v", deltas)
	}
}

func TestCreateStream_ContextCancel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"))
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: upstream.URL,
		endpoint:    "responses",
		providers:   []string{"test-openai"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := env.service.CreateStream(ctx, env.identity, &provider.ResponseRequest{
		Model:  "public-model",
		Input:  "hello",
		Stream: true,
	}, "session-cancel")
	if err != nil {
		t.Fatalf("CreateStream() error: %v", err)
	}

	var sawDelta bool
	var streamErr error
	for eventsCh := stream.Events; eventsCh != nil; {
		select {
		case e, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				continue
			}
			if e.Delta == "partial" {
				sawDelta = true
				cancel()
			}
		case err := <-stream.Errors:
			if err != nil {
				streamErr = err
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out")
		}
	}
	if !sawDelta {
		t.Fatal("expected delta before cancel")
	}
	if !errors.Is(streamErr, context.Canceled) {
		t.Fatalf("stream error = %v, want context.Canceled", streamErr)
	}
}
