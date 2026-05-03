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

func TestCloneStringAnyMapWithJSON(t *testing.T) {
	m := map[string]any{"a": float64(1), "b": "two"}
	got := cloneStringAnyMap(m)
	if len(got) != 2 {
		t.Fatalf("cloneStringAnyMap() len = %d, want 2", len(got))
	}
	if got["a"] != float64(1) {
		t.Fatalf("cloneStringAnyMap() a = %v, want 1", got["a"])
	}
	got["a"] = float64(99)
	if m["a"] != float64(1) {
		t.Fatal("cloneStringAnyMap mutated original")
	}
}

func TestCloneStringAnyMapEmpty(t *testing.T) {
	if cloneStringAnyMap(nil) != nil {
		t.Fatal("expected nil for nil input")
	}
	if cloneStringAnyMap(map[string]any{}) != nil {
		t.Fatal("expected nil for empty input")
	}
}

func TestFirstNonEmptyLocal(t *testing.T) {
	if got := firstNonEmptyLocal("", "a", "b"); got != "a" {
		t.Fatalf("firstNonEmptyLocal = %q, want a", got)
	}
	if got := firstNonEmptyLocal("x", "y"); got != "x" {
		t.Fatalf("firstNonEmptyLocal = %q, want x", got)
	}
	if got := firstNonEmptyLocal("", ""); got != "" {
		t.Fatalf("firstNonEmptyLocal = %q, want empty", got)
	}
}

func TestAppendStreamedToolCalls(t *testing.T) {
	calls := []provider.ToolCall{
		{ID: "c1", Function: provider.FunctionCall{Name: "fn1", Arguments: `{"a":1}`}},
	}
	outputs := appendStreamedToolCalls(nil, calls)
	if len(outputs) != 1 || outputs[0].Type != "function_call" {
		t.Fatalf("unexpected outputs: %v", outputs)
	}
}

func TestAppendStreamOutputDeduplicates(t *testing.T) {
	outputs := []provider.ResponseOutput{
		{ID: "c1", Type: "function_call", CallID: "c1"},
	}
	dup := &provider.ResponseOutput{ID: "c1", Type: "function_call", CallID: "c1"}
	got := appendStreamOutput(outputs, dup)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (dedup)", len(got))
	}
}

func TestAppendStreamOutputAppendsNew(t *testing.T) {
	outputs := []provider.ResponseOutput{
		{ID: "c1", Type: "function_call", CallID: "c1"},
	}
	newOut := &provider.ResponseOutput{ID: "c2", Type: "function_call", CallID: "c2"}
	got := appendStreamOutput(outputs, newOut)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestAppendStreamOutputNil(t *testing.T) {
	outputs := []provider.ResponseOutput{{ID: "c1", Type: "message"}}
	got := appendStreamOutput(outputs, nil)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}

func TestErrorString(t *testing.T) {
	if errorString(nil) != "" {
		t.Fatal("errorString(nil) != empty")
	}
	if errorString(errors.New("boom")) != "boom" {
		t.Fatal("errorString mismatch")
	}
}

func TestGetCircuitBreakerStates(t *testing.T) {
	svc := New(&Dependencies{Config: &config.Config{CircuitBreaker: config.CircuitBreakerConfig{FailureThreshold: 1}}})
	svc.circuitBreaker = nil
	if svc.GetCircuitBreakerStates() != nil {
		t.Fatal("expected nil when no circuit breaker")
	}
}

func TestGetCircuitBreakerStatesWithCB(t *testing.T) {
	cfg := config.CircuitBreakerConfig{FailureThreshold: 1}
	svc := New(&Dependencies{Config: &config.Config{CircuitBreaker: cfg}})
	svc.circuitBreaker.RecordFailure("t1", "p1")
	states := svc.GetCircuitBreakerStates()
	if len(states) != 1 {
		t.Fatalf("len(states) = %d, want 1", len(states))
	}
}

func TestEmitStreamPayloadFromResponseToolCall(t *testing.T) {
	resp := &provider.Response{
		Output: []provider.ResponseOutput{{
			Type:   "function_call",
			ID:     "c1",
			CallID: "c1",
			Name:   "fn1",
			Args:   `{}`,
		}},
	}
	out := make(chan provider.ResponseEvent, 4)
	svc := &Service{}
	svc.emitStreamPayloadFromResponse(out, resp)
	close(out)

	var events []string
	for e := range out {
		events = append(events, e.Type)
	}
	if len(events) != 1 || events[0] != provider.EventToolCallDone {
		t.Fatalf("unexpected events: %v", events)
	}
}

func TestEmitStreamPayloadFromResponseNil(t *testing.T) {
	out := make(chan provider.ResponseEvent, 1)
	svc := &Service{}
	svc.emitStreamPayloadFromResponse(out, nil)
	close(out)
	if len(out) != 0 {
		t.Fatal("expected no events for nil resp")
	}
}

func TestBuildAccumulatedStreamResponseEmpty(t *testing.T) {
	resp := buildAccumulatedStreamResponse("r1", "m1", "", nil, 5)
	if resp.Usage.TotalTokens != 5 {
		t.Fatalf("TotalTokens = %d, want 5", resp.Usage.TotalTokens)
	}
}

func TestBuildAccumulatedStreamResponseWithText(t *testing.T) {
	resp := buildAccumulatedStreamResponse("r1", "m1", "hello", nil, 5)
	if resp.OutputText() != "hello" {
		t.Fatalf("OutputText = %q, want hello", resp.OutputText())
	}
	if resp.Usage.PromptTokens != 5 {
		t.Fatalf("PromptTokens = %d, want 5", resp.Usage.PromptTokens)
	}
}

func TestIsRenderableStreamEvent(t *testing.T) {
	if !isRenderableStreamEvent(provider.ResponseEvent{Delta: "hi"}) {
		t.Fatal("expected true for text delta")
	}
	if !isRenderableStreamEvent(provider.ResponseEvent{ToolCalls: []provider.ToolCall{{ID: "c1"}}}) {
		t.Fatal("expected true for tool calls")
	}
	if isRenderableStreamEvent(provider.ResponseEvent{}) {
		t.Fatal("expected false for empty event")
	}
}

func TestHasRenderableStreamPayload(t *testing.T) {
	if hasRenderableStreamPayload(nil) {
		t.Fatal("expected false for nil")
	}
	resp := &provider.Response{Output: []provider.ResponseOutput{{
		Type:    "message",
		Content: []provider.ResponseContent{{Type: "output_text", Text: "hi"}},
	}}}
	if !hasRenderableStreamPayload(resp) {
		t.Fatal("expected true for text output")
	}
}

func TestPrepareReturnsExecution(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	exec, err := env.service.prepare(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model", Input: "hi",
	}, "s1")
	if err != nil {
		t.Fatalf("prepare() error: %v", err)
	}
	if exec == nil || exec.provider == nil {
		t.Fatal("expected non-nil execution")
	}
}

func TestPrepareReturnsErrNoProvider(t *testing.T) {
	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: "http://127.0.0.1:1",
		providers:   []string{},
	})

	_, err := env.service.prepare(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model", Input: "hi",
	}, "s1")
	if !errors.Is(err, ErrNoProvider) {
		t.Fatalf("prepare() error = %v, want %v", err, ErrNoProvider)
	}
}

func TestPrepareWithProviderCreatesRecord(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	p := env.providerMgr.List()[0]
	exec, err := env.service.prepareWithProvider(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model", Input: "hi",
	}, "s1", p)
	if err != nil {
		t.Fatalf("prepareWithProvider() error: %v", err)
	}
	if exec.provider.Name() != "test-openai" {
		t.Fatalf("provider = %q, want test-openai", exec.provider.Name())
	}

	record, _ := env.store.GetResponse(context.Background(), env.identity.TenantID, exec.responseID)
	if record.Status != "in_progress" {
		t.Fatalf("status = %q, want in_progress", record.Status)
	}
}

func TestRunStreamSuccess(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"streamed\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"created_at\":1,\"model\":\"m\",\"status\":\"completed\",\"output\":[{\"id\":\"msg\",\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"streamed\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "responses",
		providers:   []string{"test-openai"},
	})

	exec, _ := env.service.prepareWithProvider(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model", Input: "hi", Stream: true,
	}, "s1", env.providerMgr.List()[0])

	out := make(chan provider.ResponseEvent, 8)
	errCh := make(chan error, 1)
	env.service.runStream(context.Background(), env.identity, exec, out, errCh)

	var types []string
	for e := range out {
		types = append(types, e.Type)
	}
	if len(types) < 2 || types[0] != provider.EventResponseStarted {
		t.Fatalf("unexpected events: %v", types)
	}
}

func TestRunStreamUpstreamError(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer up.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{
		upstreamURL: up.URL,
		endpoint:    "chat",
		providers:   []string{"test-openai"},
	})

	exec, _ := env.service.prepareWithProvider(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model", Input: "hi", Stream: true,
	}, "s1", env.providerMgr.List()[0])

	out := make(chan provider.ResponseEvent, 4)
	errCh := make(chan error, 1)
	env.service.runStream(context.Background(), env.identity, exec, out, errCh)

	var streamErr error
	for err := range errCh {
		if err != nil {
			streamErr = err
		}
	}
	if streamErr == nil {
		t.Fatal("expected error from upstream failure")
	}
}

func TestTouchUpdatesTimestamp(t *testing.T) {
	tr := &routeTrace{Status: "planned"}
	before := tr.UpdatedAt
	tr.touch()
	if tr.UpdatedAt == before {
		t.Fatal("touch did not update UpdatedAt")
	}
}

func TestTouchNoOpForNil(t *testing.T) {
	var tr *routeTrace
	tr.touch() // should not panic
}

func TestCircuitBreaker_IsAvailable_Open(t *testing.T) {
	cb := NewCircuitBreaker(config.CircuitBreakerConfig{FailureThreshold: 1, RecoveryTimeout: 1})
	cb.RecordFailure("t1", "p1")
	if cb.IsAvailable("t1", "p1") {
		t.Fatal("expected unavailable when open")
	}
}

func TestCircuitBreaker_IsAvailable_Closed(t *testing.T) {
	cb := NewCircuitBreaker(config.CircuitBreakerConfig{FailureThreshold: 3})
	if !cb.IsAvailable("t1", "p1") {
		t.Fatal("expected available when closed")
	}
}

func TestCircuitBreaker_RecordSuccess_Reset(t *testing.T) {
	cb := NewCircuitBreaker(config.CircuitBreakerConfig{FailureThreshold: 2})
	cb.RecordFailure("t1", "p1")
	cb.RecordFailure("t1", "p1")
	cb.RecordSuccess("t1", "p1")
	if !cb.IsAvailable("t1", "p1") {
		t.Fatal("expected available after success reset")
	}
}

func TestCircuitBreaker_GetAllStates(t *testing.T) {
	cb := NewCircuitBreaker(config.CircuitBreakerConfig{FailureThreshold: 1})
	cb.RecordFailure("t1", "p1")
	states := cb.GetAllStates()
	if len(states) != 1 {
		t.Fatalf("len(states) = %d, want 1", len(states))
	}
}

func TestBuildCacheKey_WithTools(t *testing.T) {
	svc := New(&Dependencies{Config: &config.Config{}})
	req := &provider.ResponseRequest{
		Model:  "gpt-4",
		Input:  "hello",
		Stream: false,
		Tools:  []any{map[string]any{"type": "function", "name": "fn1"}},
	}
	identity := &repository.AuthIdentity{TenantID: "t1", UserID: "u1"}
	k1 := svc.buildCacheKey(identity, req)
	k2 := svc.buildCacheKey(identity, req)
	if k1 != k2 {
		t.Fatal("cache key should be deterministic")
	}
}

func TestBuildCacheKey_StreamDiffers(t *testing.T) {
	svc := New(&Dependencies{Config: &config.Config{}})
	identity := &repository.AuthIdentity{TenantID: "t1", UserID: "u1"}
	req1 := &provider.ResponseRequest{Model: "gpt-4", Input: "hi", Stream: false}
	req2 := &provider.ResponseRequest{Model: "gpt-4", Input: "hi", Stream: true}
	if svc.buildCacheKey(identity, req1) == svc.buildCacheKey(identity, req2) {
		t.Fatal("stream flag should affect cache key")
	}
}
