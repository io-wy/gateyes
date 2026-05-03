package responses

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
)

type streamMockProvider struct {
	name      string
	modelName string
	failErr   error
	events    []provider.ResponseEvent
}

func (m *streamMockProvider) Name() string                     { return m.name }
func (m *streamMockProvider) Type() string                     { return "openai" }
func (m *streamMockProvider) BaseURL() string                  { return "" }
func (m *streamMockProvider) Model() string                    { return m.modelName }
func (m *streamMockProvider) Weight() int                      { return 1 }
func (m *streamMockProvider) UnitCost() float64                { return 0 }
func (m *streamMockProvider) Cost(_, _ int) float64            { return 0 }
func (m *streamMockProvider) CreateEmbedding(context.Context, *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, errors.New("not implemented")
}
func (m *streamMockProvider) CreateResponse(context.Context, *provider.ResponseRequest) (*provider.Response, error) {
	return nil, errors.New("not implemented")
}
func (m *streamMockProvider) StreamResponse(_ context.Context, _ *provider.ResponseRequest) (<-chan provider.ResponseEvent, <-chan error) {
	out := make(chan provider.ResponseEvent, len(m.events))
	errCh := make(chan error, 1)
	for _, e := range m.events {
		out <- e
	}
	close(out)
	if m.failErr != nil {
		errCh <- m.failErr
	}
	close(errCh)
	return out, errCh
}

func TestRunStreamWithFallback_Success(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"created_at\":1,\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
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
		Input:  "hi",
		Stream: true,
	}, "s1")
	if err != nil {
		t.Fatalf("CreateStream() error: %v", err)
	}

	var types []string
	for e := range stream.Events {
		types = append(types, e.Type)
	}
	if len(types) < 2 || types[0] != provider.EventResponseStarted {
		t.Fatalf("unexpected events: %v", types)
	}
}

func TestRunStreamWithFallback_Retry(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"created_at\":1,\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"retry-ok\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
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
		Input:  "hi",
		Stream: true,
	}, "s1")
	if err != nil {
		t.Fatalf("CreateStream() error: %v", err)
	}

	var deltas []string
	for e := range stream.Events {
		if e.Text() != "" {
			deltas = append(deltas, e.Text())
		}
	}
	if len(deltas) == 0 || deltas[0] != "retry-ok" {
		t.Fatalf("unexpected deltas: %v", deltas)
	}
}

func TestFinalizeStream(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	_ = env.store.CreateResponse(context.Background(), repository.ResponseRecord{
		ID:       "resp-1",
		TenantID: env.identity.TenantID,
		Status:   "in_progress",
	})

	resp := provider.NewTextResponse("resp-1", "public-model", "final", provider.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3})
	out := make(chan provider.ResponseEvent, 4)
	trace := &routeTrace{}
	env.service.finalizeStream(context.Background(), env.identity, "resp-1", "test-openai", "public-model", resp, 100, trace, out, true)
	close(out)

	var events []string
	for e := range out {
		events = append(events, e.Type)
	}
	if len(events) != 2 || events[0] != provider.EventContentDelta || events[1] != provider.EventResponseCompleted {
		t.Fatalf("unexpected events: %v", events)
	}

	record, err := env.store.GetResponse(context.Background(), env.identity.TenantID, "resp-1")
	if err != nil {
		t.Fatalf("GetResponse() error: %v", err)
	}
	if record.Status != "completed" {
		t.Fatalf("status = %q, want completed", record.Status)
	}
}

func TestRecoverStreamResponse(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"recovered"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	exec := &execution{
		provider:              env.providerMgr.List()[0],
		requestedModel:        "public-model",
		upstreamRequest:       &provider.ResponseRequest{Model: "public-model"},
		responseID:            "resp-1",
		estimatedPromptTokens: 2,
	}
	got := env.service.recoverStreamResponse(context.Background(), env.identity, exec, "accumulated", nil, nil, false)
	// callWithRetry succeeds because the upstream mock returns a response, so recovered takes precedence
	if got == nil || got.OutputText() != "recovered" {
		t.Fatalf("recoverStreamResponse() text = %q, want recovered", got.OutputText())
	}
}

func TestEmitStreamPayloadFromResponse(t *testing.T) {
	resp := provider.NewTextResponse("r1", "m1", "hello world", provider.Usage{})
	out := make(chan provider.ResponseEvent, 4)
	svc := &Service{}
	svc.emitStreamPayloadFromResponse(out, resp)
	close(out)

	var deltas []string
	for e := range out {
		deltas = append(deltas, e.Delta)
	}
	if len(deltas) != 1 || deltas[0] != "hello world" {
		t.Fatalf("unexpected deltas: %v", deltas)
	}
}

func TestHandleStreamError(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	_ = env.store.CreateResponse(context.Background(), repository.ResponseRecord{
		ID:       "resp-err",
		TenantID: env.identity.TenantID,
		Status:   "in_progress",
	})
	env.service.handleStreamError(context.Background(), env.identity, "resp-err", "test-openai", "public-model", 50, errors.New("boom"))

	record, _ := env.store.GetResponse(context.Background(), env.identity.TenantID, "resp-err")
	if record.Status != "error" {
		t.Fatalf("status = %q, want error", record.Status)
	}
}

func TestIsStreamRetryable(t *testing.T) {
	svc := &Service{}
	if !svc.isStreamRetryable(errors.New("upstream returned 429")) {
		t.Fatal("isStreamRetryable(429) = false, want true")
	}
	if !svc.isStreamRetryable(errors.New("upstream returned 500")) {
		t.Fatal("isStreamRetryable(500) = false, want true")
	}
	if svc.isStreamRetryable(errors.New("upstream returned 400")) {
		t.Fatal("isStreamRetryable(400) = true, want false")
	}
}
