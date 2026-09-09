package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/service/guardrail"
	"github.com/gateyes/gateway/internal/service/provider"
)

func TestClassifyToolsReturnsThreeOwners(t *testing.T) {
	svc := &Service{cfg: &config.Config{ToolOwnership: config.ToolOwnershipConfig{
		Enabled: true,
		Rules: []config.ToolOwnershipRule{
			{Name: "search", Owner: "gateway-owned"},
			{Name: "confirm", Owner: "client-owned"},
			{Type: "web_search", Owner: "provider-owned"},
		},
	}}}
	req := &provider.ResponseRequest{Tools: []any{
		map[string]any{"type": "function", "name": "search"},
		map[string]any{"type": "function", "function": map[string]any{"name": "confirm"}},
		map[string]any{"type": "web_search"},
	}}

	decisions, err := svc.classifyTools(req)
	if err != nil {
		t.Fatalf("classifyTools() error: %v", err)
	}
	want := []ToolOwner{ToolOwnerGateway, ToolOwnerClient, ToolOwnerProvider}
	if len(decisions) != len(want) {
		t.Fatalf("decisions len = %d, want %d", len(decisions), len(want))
	}
	for i := range want {
		if decisions[i].Owner != want[i] {
			t.Fatalf("decisions[%d].Owner = %q, want %q", i, decisions[i].Owner, want[i])
		}
	}
}

func TestClassifyToolsRejectsUnknownAmbiguousAndUnknownShape(t *testing.T) {
	svc := &Service{cfg: &config.Config{ToolOwnership: config.ToolOwnershipConfig{
		Enabled: true,
		Rules: []config.ToolOwnershipRule{
			{Name: "lookup", Owner: "gateway-owned"},
			{Type: "function", Owner: "client-owned"},
		},
	}}}
	for _, tc := range []struct {
		name string
		tool any
	}{
		{name: "unknown", tool: map[string]any{"type": "custom"}},
		{name: "ambiguous", tool: map[string]any{"type": "function", "name": "lookup"}},
		{name: "unknown shape", tool: "lookup"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.classifyTools(&provider.ResponseRequest{Tools: []any{tc.tool}})
			if !errors.Is(err, ErrInvalidToolOwnership) {
				t.Fatalf("classifyTools() error = %v, want ErrInvalidToolOwnership", err)
			}
		})
	}
}

func TestValidateToolOwnershipRejectsMissingGatewayExecutor(t *testing.T) {
	svc := &Service{cfg: &config.Config{ToolOwnership: config.ToolOwnershipConfig{
		Enabled: true, Rules: []config.ToolOwnershipRule{{Name: "lookup", Owner: "gateway-owned"}},
	}}}
	_, err := svc.validateToolOwnership(&provider.ResponseRequest{Tools: []any{
		map[string]any{"type": "function", "name": "lookup"},
	}})
	if !errors.Is(err, ErrToolExecutorUnavailable) {
		t.Fatalf("validateToolOwnership() error = %v, want ErrToolExecutorUnavailable", err)
	}
}

func TestRunGatewayToolLoopExecutesAndContinues(t *testing.T) {
	executor := &fakeToolExecutor{result: `{"temperature":24}`}
	p := &toolLoopProvider{responses: []*provider.Response{{
		Status: "completed",
		Usage:  provider.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7, CachedTokens: 1},
		Output: []provider.ResponseOutput{{
			Type: "message", Role: "assistant",
			Content: []provider.ResponseContent{{Type: "output_text", Text: "It is 24C"}},
		}},
	}}}
	svc := &Service{
		cfg:          &config.Config{ToolOwnership: config.ToolOwnershipConfig{Enabled: true, MaxLoopRounds: 2}},
		toolExecutor: executor,
	}
	req := &provider.ResponseRequest{Model: "m", Input: "weather?"}
	initial := toolCallResponse("call-1", "weather")
	initial.Usage = provider.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}
	trace := &routeTrace{}

	got, err := svc.runGatewayToolLoop(context.Background(), &execution{provider: p, requestedModel: "m", responseID: "r1"}, req, initial, map[string]ToolOwner{"weather": ToolOwnerGateway}, trace)
	if err != nil {
		t.Fatalf("runGatewayToolLoop() error: %v", err)
	}
	if got.OutputText() != "It is 24C" || len(executor.calls) != 1 || len(p.requests) != 1 {
		t.Fatalf("result=%q executor=%+v provider_calls=%d", got.OutputText(), executor.calls, len(p.requests))
	}
	if len(p.requests[0].InputMessages()) != 3 || len(req.InputMessages()) != 1 {
		t.Fatalf("continuation/original message counts = %d/%d, want 3/1", len(p.requests[0].InputMessages()), len(req.InputMessages()))
	}
	if len(trace.ToolCalls) != 1 || trace.ToolCalls[0].Outcome != "success" {
		t.Fatalf("trace tool calls = %+v, want success", trace.ToolCalls)
	}
	if got.Usage != (provider.Usage{PromptTokens: 7, CompletionTokens: 3, TotalTokens: 10, CachedTokens: 1}) {
		t.Fatalf("aggregated usage = %+v, want 7 prompt / 3 completion / 10 total / 1 cached", got.Usage)
	}
}

func TestToolLoopResponseMessagesPreservesAssistantContentAndCallsInOneTurn(t *testing.T) {
	resp := toolCallResponse("call-1", "weather")
	resp.Output = append([]provider.ResponseOutput{{
		Type: "message", Role: "assistant",
		Content: []provider.ResponseContent{{Type: "output_text", Text: "Let me check."}},
	}}, resp.Output...)

	messages := toolLoopResponseMessages(resp)
	if len(messages) != 1 {
		t.Fatalf("toolLoopResponseMessages() len = %d, want one assistant turn", len(messages))
	}
	var got string
	for _, block := range messages[0].Content {
		got += block.Text
	}
	if got != "Let me check." {
		t.Fatalf("assistant content = %q, want preserved text", got)
	}
	if len(messages[0].ToolCalls) != 1 || messages[0].ToolCalls[0].Function.Name != "weather" {
		t.Fatalf("assistant tool calls = %+v, want weather", messages[0].ToolCalls)
	}
}

func TestCreateRevalidatesToolsAfterGuardrailTransform(t *testing.T) {
	transformed := &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
		Tools: []any{map[string]any{"type": "custom"}},
	}
	env := newResponsesTestEnv(t, responsesTestEnvConfig{endpoint: "chat", providers: []string{"test-openai"}})
	env.service.cfg.ToolOwnership = config.ToolOwnershipConfig{
		Enabled: true,
		Rules:   []config.ToolOwnershipRule{{Name: "confirm", Owner: "client-owned"}},
	}
	env.service.guardrails = guardrail.New([]guardrail.Guardrail{transformingToolGuardrail{request: transformed}})

	_, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model",
		Input: "hello",
		Tools: []any{map[string]any{"type": "function", "name": "confirm"}},
	}, "")
	if !errors.Is(err, ErrInvalidToolOwnership) {
		t.Fatalf("Create() error = %v, want ErrInvalidToolOwnership after guardrail transform", err)
	}
}

func TestRunGatewayToolLoopRejectsFailureModes(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		executor *fakeToolExecutor
		provider *toolLoopProvider
		owners   map[string]ToolOwner
		want     error
	}{
		{name: "executor error", ctx: context.Background(), executor: &fakeToolExecutor{err: errors.New("boom")}, provider: &toolLoopProvider{}, owners: map[string]ToolOwner{"server": ToolOwnerGateway}, want: errors.New("boom")},
		{name: "cancelled", ctx: cancelledContext(), executor: &fakeToolExecutor{checkContext: true}, provider: &toolLoopProvider{}, owners: map[string]ToolOwner{"server": ToolOwnerGateway}, want: context.Canceled},
		{name: "mixed owners", ctx: context.Background(), executor: &fakeToolExecutor{}, provider: &toolLoopProvider{}, owners: map[string]ToolOwner{"server": ToolOwnerGateway, "client": ToolOwnerClient}, want: ErrInvalidToolOwnership},
		{name: "round limit", ctx: context.Background(), executor: &fakeToolExecutor{result: `{}`}, provider: &toolLoopProvider{responses: []*provider.Response{toolCallResponse("call-2", "server")}}, owners: map[string]ToolOwner{"server": ToolOwnerGateway}, want: ErrToolLoopLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{cfg: &config.Config{ToolOwnership: config.ToolOwnershipConfig{Enabled: true, MaxLoopRounds: 1}}, toolExecutor: tt.executor}
			initial := toolCallResponse("call-1", "server")
			if tt.name == "mixed owners" {
				initial.Output = append(initial.Output, toolCallResponse("call-2", "client").Output...)
			}
			_, err := svc.runGatewayToolLoop(tt.ctx, &execution{provider: tt.provider, requestedModel: "m", responseID: "r1"}, &provider.ResponseRequest{Model: "m", Input: "go"}, initial, tt.owners, &routeTrace{})
			if !errors.Is(err, tt.want) && !strings.Contains(fmt.Sprint(err), tt.want.Error()) {
				t.Fatalf("runGatewayToolLoop() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCreateReturnsClientOwnedToolCallAndTracesOwner(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"first","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"confirm","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
	}))
	defer upstream.Close()
	env := newResponsesTestEnv(t, responsesTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat", providers: []string{"test-openai"}})
	env.service.cfg.ToolOwnership = config.ToolOwnershipConfig{Enabled: true, Rules: []config.ToolOwnershipRule{{Name: "confirm", Owner: "client-owned"}}}

	result, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model", Input: "confirm?", Tools: []any{map[string]any{"type": "function", "name": "confirm"}},
	}, "")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if calls := result.Response.OutputToolCalls(); len(calls) != 1 || calls[0].Function.Name != "confirm" {
		t.Fatalf("client tool calls = %+v, want confirm", calls)
	}
	record, _ := env.store.GetResponse(context.Background(), env.identity.TenantID, result.Response.ID)
	var trace routeTrace
	if err := json.Unmarshal(record.RouteTraceBody, &trace); err != nil {
		t.Fatalf("decode route trace: %v", err)
	}
	if len(trace.ToolCalls) != 1 || trace.ToolCalls[0].Owner != ToolOwnerClient || trace.ToolCalls[0].Outcome != "returned" {
		t.Fatalf("trace.ToolCalls = %+v, want client-owned returned", trace.ToolCalls)
	}
}

func TestCreateReturnsProviderOwnedToolCallAndTracesOwner(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"first","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
	}))
	defer upstream.Close()
	env := newResponsesTestEnv(t, responsesTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat", providers: []string{"test-openai"}})
	env.service.cfg.ToolOwnership = config.ToolOwnershipConfig{Enabled: true, Rules: []config.ToolOwnershipRule{{Name: "lookup", Owner: "provider-owned"}}}

	result, err := env.service.Create(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model", Input: "lookup", Tools: []any{map[string]any{"type": "function", "name": "lookup"}},
	}, "")
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if calls := result.Response.OutputToolCalls(); len(calls) != 1 || calls[0].Function.Name != "lookup" {
		t.Fatalf("provider tool calls = %+v, want lookup", calls)
	}
	record, _ := env.store.GetResponse(context.Background(), env.identity.TenantID, result.Response.ID)
	var trace routeTrace
	if err := json.Unmarshal(record.RouteTraceBody, &trace); err != nil {
		t.Fatalf("decode route trace: %v", err)
	}
	if len(trace.ToolDefinitions) != 1 || trace.ToolDefinitions[0].Owner != ToolOwnerProvider {
		t.Fatalf("trace.ToolDefinitions = %+v, want provider-owned", trace.ToolDefinitions)
	}
	if len(trace.ToolCalls) != 1 || trace.ToolCalls[0].Owner != ToolOwnerProvider || trace.ToolCalls[0].Outcome != "returned" {
		t.Fatalf("trace.ToolCalls = %+v, want provider-owned returned", trace.ToolCalls)
	}
}

func TestCreateStreamExecutesGatewayOwnedToolBeforeEvents(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			_, _ = fmt.Fprint(w, `{"id":"first","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"weather","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"second","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"24C"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`)
	}))
	defer upstream.Close()
	env := newResponsesTestEnv(t, responsesTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat", providers: []string{"test-openai"}})
	env.service.cfg.ToolOwnership = config.ToolOwnershipConfig{Enabled: true, MaxLoopRounds: 2, Rules: []config.ToolOwnershipRule{{Name: "weather", Owner: "gateway-owned"}}}
	env.service.SetToolExecutor(&fakeToolExecutor{result: `{}`})

	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model: "public-model", Input: "weather?", Stream: true, Tools: []any{map[string]any{"type": "function", "name": "weather"}},
	}, "")
	if err != nil {
		t.Fatalf("CreateStream() error: %v", err)
	}
	var text string
	for stream.Events != nil || stream.Errors != nil {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				stream.Events = nil
				continue
			}
			if len(event.ToolCalls) > 0 || event.Output != nil && event.Output.Type == "function_call" {
				t.Fatalf("client received gateway-owned call: %+v", event)
			}
			text += event.Text()
		case err, ok := <-stream.Errors:
			if !ok {
				stream.Errors = nil
				continue
			}
			if err != nil {
				t.Fatalf("stream error: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for stream")
		}
	}
	if text != "24C" {
		t.Fatalf("stream text = %q, want 24C", text)
	}
	record, err := env.store.GetResponse(context.Background(), env.identity.TenantID, stream.ResponseID)
	if err != nil {
		t.Fatalf("GetResponse() error: %v", err)
	}
	var storedRequest provider.ResponseRequest
	if err := json.Unmarshal(record.RequestBody, &storedRequest); err != nil {
		t.Fatalf("decode stored request: %v", err)
	}
	if !storedRequest.Stream {
		t.Fatal("stored gateway-owned stream request lost stream=true")
	}
	var trace routeTrace
	if err := json.Unmarshal(record.RouteTraceBody, &trace); err != nil {
		t.Fatalf("decode route trace: %v", err)
	}
	if len(trace.ToolDefinitions) != 1 || trace.ToolDefinitions[0].Owner != ToolOwnerGateway {
		t.Fatalf("trace.ToolDefinitions = %+v, want gateway-owned", trace.ToolDefinitions)
	}
}

func TestCreateStreamGatewayOwnedToolHydratesPreviousResponseOnce(t *testing.T) {
	var calls atomic.Int32
	var firstMessageCount int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []provider.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode upstream request: %v", err)
		}
		if calls.Add(1) == 1 {
			firstMessageCount = len(body.Messages)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"id":"first","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call-1","type":"function","function":{"name":"weather","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"second","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"24C"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`)
	}))
	defer upstream.Close()

	env := newResponsesTestEnv(t, responsesTestEnvConfig{upstreamURL: upstream.URL, endpoint: "chat", providers: []string{"test-openai"}})
	persistResponseTurn(t, env, "resp-a", "", "earlier question", "earlier answer")
	env.service.cfg.ToolOwnership = config.ToolOwnershipConfig{Enabled: true, MaxLoopRounds: 2, Rules: []config.ToolOwnershipRule{{Name: "weather", Owner: "gateway-owned"}}}
	env.service.SetToolExecutor(&fakeToolExecutor{result: `{}`})

	stream, err := env.service.CreateStream(context.Background(), env.identity, &provider.ResponseRequest{
		Model:              "public-model",
		PreviousResponseID: "resp-a",
		Input:              "weather?",
		Stream:             true,
		Tools:              []any{map[string]any{"type": "function", "name": "weather"}},
	}, "")
	if err != nil {
		t.Fatalf("CreateStream() error: %v", err)
	}
	for range stream.Events {
	}
	for err := range stream.Errors {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
	}
	if firstMessageCount != 3 {
		t.Fatalf("first upstream message count = %d, want 3", firstMessageCount)
	}
}

type transformingToolGuardrail struct {
	request *provider.ResponseRequest
}

func (transformingToolGuardrail) Name() string { return "transform-tools" }
func (g transformingToolGuardrail) PreCall(context.Context, *provider.ResponseRequest) guardrail.PreResult {
	return guardrail.PreResult{Verdict: guardrail.Transform, Request: g.request}
}
func (transformingToolGuardrail) PostCall(_ context.Context, resp *provider.Response) guardrail.PostResult {
	return guardrail.PostResult{Verdict: guardrail.Allow, Response: resp}
}

type fakeToolExecutor struct {
	result       string
	err          error
	checkContext bool
	calls        []ToolExecution
}

func (f *fakeToolExecutor) Execute(ctx context.Context, call ToolExecution) (string, error) {
	f.calls = append(f.calls, call)
	if f.checkContext {
		return "", ctx.Err()
	}
	return f.result, f.err
}

type toolLoopProvider struct {
	responses []*provider.Response
	requests  []*provider.ResponseRequest
}

func (p *toolLoopProvider) Name() string              { return "tool-loop" }
func (p *toolLoopProvider) Type() string              { return "mock" }
func (p *toolLoopProvider) BaseURL() string           { return "" }
func (p *toolLoopProvider) Model() string             { return "m" }
func (p *toolLoopProvider) Labels() map[string]string { return nil }
func (p *toolLoopProvider) Weight() int               { return 1 }
func (p *toolLoopProvider) UnitCost() float64         { return 0 }
func (p *toolLoopProvider) Cost(int, int) float64     { return 0 }
func (p *toolLoopProvider) CreateResponse(_ context.Context, req *provider.ResponseRequest) (*provider.Response, error) {
	p.requests = append(p.requests, req)
	if len(p.responses) == 0 {
		return nil, errors.New("no response")
	}
	resp := p.responses[0]
	p.responses = p.responses[1:]
	return resp, nil
}
func (p *toolLoopProvider) StreamResponse(context.Context, *provider.ResponseRequest) (<-chan provider.ResponseEvent, <-chan error) {
	return nil, nil
}
func (p *toolLoopProvider) CreateEmbedding(context.Context, *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, nil
}
func (p *toolLoopProvider) CreateImageGeneration(context.Context, *provider.ImageGenerationRequest) (*provider.ImageGenerationResponse, error) {
	return nil, nil
}

func toolCallResponse(id, name string) *provider.Response {
	return &provider.Response{Status: "completed", Output: []provider.ResponseOutput{{
		ID: id, CallID: id, Type: "function_call", Name: name, Args: `{}`,
	}}}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
