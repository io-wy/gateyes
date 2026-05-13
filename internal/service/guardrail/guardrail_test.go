package guardrail

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/service/provider"
)

func TestRegexBlocklistBlocksMatchingPrompt(t *testing.T) {
	g := NewRegexBlocklist("naive-pii", []string{`\bSSN[: ]+\d{3}-\d{2}-\d{4}\b`}, nil)
	req := &provider.ResponseRequest{Input: "my SSN: 123-45-6789 please don't store"}
	res := g.PreCall(context.Background(), req)
	if res.Verdict != Block {
		t.Fatalf("verdict = %v, want Block", res.Verdict)
	}
	if res.Reason == "" {
		t.Fatal("reason empty")
	}
}

func TestRegexBlocklistAllowsBenignPrompt(t *testing.T) {
	g := NewRegexBlocklist("test", []string{`forbidden`}, nil)
	req := &provider.ResponseRequest{Input: "hello there"}
	res := g.PreCall(context.Background(), req)
	if res.Verdict != Allow {
		t.Fatalf("verdict = %v, want Allow", res.Verdict)
	}
}

func TestRegexBlocklistBlocksMatchingResponse(t *testing.T) {
	g := NewRegexBlocklist("test", nil, []string{`hidden token`})
	resp := &provider.Response{
		Output: []provider.ResponseOutput{{
			Type: "message",
			Content: []provider.ResponseContent{{Type: "output_text", Text: "the hidden token is X"}},
		}},
	}
	res := g.PostCall(context.Background(), resp)
	if res.Verdict != Block {
		t.Fatalf("verdict = %v, want Block", res.Verdict)
	}
}

func TestRegexBlocklistInvalidPatternIsSkipped(t *testing.T) {
	// invalid regex should not panic — skipped silently
	g := NewRegexBlocklist("test", []string{`[invalid(regex`}, nil)
	if len(g.requestRe) != 0 {
		t.Fatalf("expected invalid pattern to be dropped, got %d compiled", len(g.requestRe))
	}
	res := g.PreCall(context.Background(), &provider.ResponseRequest{Input: "ok"})
	if res.Verdict != Allow {
		t.Fatalf("verdict = %v, want Allow when no patterns", res.Verdict)
	}
}

type stubGuardrail struct {
	name      string
	pre       PreResult
	postFn    func(*provider.Response) PostResult
}

func (g *stubGuardrail) Name() string { return g.name }
func (g *stubGuardrail) PreCall(_ context.Context, req *provider.ResponseRequest) PreResult {
	if g.pre.Request == nil {
		g.pre.Request = req
	}
	return g.pre
}
func (g *stubGuardrail) PostCall(_ context.Context, resp *provider.Response) PostResult {
	if g.postFn != nil {
		return g.postFn(resp)
	}
	return PostResult{Verdict: Allow, Response: resp}
}

func TestManagerStopsAtFirstBlock(t *testing.T) {
	allow := &stubGuardrail{name: "a", pre: PreResult{Verdict: Allow}}
	block := &stubGuardrail{name: "b", pre: PreResult{Verdict: Block, Reason: "no"}}
	never := &stubGuardrail{name: "c", pre: PreResult{Verdict: Block, Reason: "should not run"}}
	m := New([]Guardrail{allow, block, never})

	res := m.PreCall(context.Background(), &provider.ResponseRequest{Input: "x"})
	if res.Verdict != Block || res.Reason != "no" {
		t.Fatalf("got %+v, want Block reason=no", res)
	}
}

func TestManagerEmptyChainAllows(t *testing.T) {
	m := New(nil)
	res := m.PreCall(context.Background(), &provider.ResponseRequest{Input: "x"})
	if res.Verdict != Allow {
		t.Fatalf("verdict = %v, want Allow on empty chain", res.Verdict)
	}
}

func TestManagerTransformPropagates(t *testing.T) {
	rewritten := &provider.ResponseRequest{Input: "rewritten"}
	transform := &stubGuardrail{name: "rewrite", pre: PreResult{Verdict: Transform, Request: rewritten}}
	check := &stubGuardrail{name: "check", pre: PreResult{Verdict: Allow}}
	m := New([]Guardrail{transform, check})
	res := m.PreCall(context.Background(), &provider.ResponseRequest{Input: "original"})
	if res.Verdict != Allow {
		t.Fatalf("verdict = %v, want Allow", res.Verdict)
	}
	if res.Request == nil || res.Request.Input != "rewritten" {
		t.Fatalf("Request not propagated: %+v", res.Request)
	}
	if check.pre.Request == nil || check.pre.Request.Input != "rewritten" {
		t.Fatalf("downstream guardrail saw original instead of rewritten: %+v", check.pre.Request)
	}
}

func TestManagerPostCallBlocksMatchingResponse(t *testing.T) {
	allow := &stubGuardrail{name: "a", postFn: func(r *provider.Response) PostResult { return PostResult{Verdict: Allow} }}
	block := &stubGuardrail{name: "b", postFn: func(r *provider.Response) PostResult {
		return PostResult{Verdict: Block, Reason: "bad output"}
	}}
	m := New([]Guardrail{allow, block})
	res := m.PostCall(context.Background(), &provider.Response{})
	if res.Verdict != Block || res.Reason != "bad output" {
		t.Fatalf("got %+v, want Block reason=bad output", res)
	}
}

func TestManagerPostCallAllowsCleanResponse(t *testing.T) {
	allow := &stubGuardrail{name: "a", postFn: func(r *provider.Response) PostResult { return PostResult{Verdict: Allow} }}
	m := New([]Guardrail{allow})
	res := m.PostCall(context.Background(), &provider.Response{ID: "r1"})
	if res.Verdict != Allow || res.Response == nil || res.Response.ID != "r1" {
		t.Fatalf("got %+v, want Allow with original response", res)
	}
}

func TestManagerPostCallTransformPropagates(t *testing.T) {
	rewritten := &provider.Response{ID: "r2"}
	transform := &stubGuardrail{name: "rewrite", postFn: func(r *provider.Response) PostResult {
		return PostResult{Verdict: Transform, Response: rewritten}
	}}
	check := &stubGuardrail{name: "check", postFn: func(r *provider.Response) PostResult {
		if r.ID != "r2" {
			t.Fatalf("downstream saw %s, want r2", r.ID)
		}
		return PostResult{Verdict: Allow}
	}}
	m := New([]Guardrail{transform, check})
	res := m.PostCall(context.Background(), &provider.Response{ID: "r1"})
	if res.Verdict != Allow || res.Response == nil || res.Response.ID != "r2" {
		t.Fatalf("got %+v, want Allow with transformed response", res)
	}
}

func TestManagerNilPostCall(t *testing.T) {
	var m *Manager
	res := m.PostCall(context.Background(), &provider.Response{ID: "r1"})
	if res.Verdict != Allow || res.Response == nil || res.Response.ID != "r1" {
		t.Fatalf("got %+v, want Allow on nil manager", res)
	}
}

func TestManagerEmptyChainPostCall(t *testing.T) {
	m := New(nil)
	res := m.PostCall(context.Background(), &provider.Response{ID: "r1"})
	if res.Verdict != Allow || res.Response == nil || res.Response.ID != "r1" {
		t.Fatalf("got %+v, want Allow on empty chain", res)
	}
}

func TestRegexBlocklistPostCallAllowsCleanResponse(t *testing.T) {
	g := NewRegexBlocklist("test", nil, []string{`badword`})
	resp := &provider.Response{
		Output: []provider.ResponseOutput{{Type: "message", Content: []provider.ResponseContent{{Type: "output_text", Text: "clean output"}}}},
	}
	res := g.PostCall(context.Background(), resp)
	if res.Verdict != Allow {
		t.Fatalf("verdict = %v, want Allow", res.Verdict)
	}
}

func TestManagerBlockDefaultReason(t *testing.T) {
	block := &stubGuardrail{name: "guard-a", pre: PreResult{Verdict: Block}}
	m := New([]Guardrail{block})
	res := m.PreCall(context.Background(), &provider.ResponseRequest{Input: "x"})
	if res.Verdict != Block {
		t.Fatalf("verdict = %v, want Block", res.Verdict)
	}
	if res.Reason != "guard-a blocked the request" {
		t.Fatalf("reason = %q, want default reason", res.Reason)
	}
}

func TestManagerPostCallBlockDefaultReason(t *testing.T) {
	block := &stubGuardrail{name: "guard-b", postFn: func(r *provider.Response) PostResult {
		return PostResult{Verdict: Block}
	}}
	m := New([]Guardrail{block})
	res := m.PostCall(context.Background(), &provider.Response{})
	if res.Verdict != Block {
		t.Fatalf("verdict = %v, want Block", res.Verdict)
	}
	if res.Reason != "guard-b blocked the response" {
		t.Fatalf("reason = %q, want default reason", res.Reason)
	}
}

func TestVerdictString(t *testing.T) {
	if Allow.String() != "allow" {
		t.Fatalf("Allow.String() = %q", Allow.String())
	}
	if Block.String() != "block" {
		t.Fatalf("Block.String() = %q", Block.String())
	}
	if Transform.String() != "transform" {
		t.Fatalf("Transform.String() = %q", Transform.String())
	}
	if Verdict(99).String() != "unknown" {
		t.Fatalf("Verdict(99).String() = %q", Verdict(99).String())
	}
}

func TestRegexBlocklistPreCallInputMessagesFallback(t *testing.T) {
	g := NewRegexBlocklist("test", []string{`badword`}, nil)
	req := &provider.ResponseRequest{
		Messages: []provider.Message{{
			Role:    "user",
			Content: []provider.ContentBlock{{Type: "text", Text: "hello badword there"}},
		}},
	}
	res := g.PreCall(context.Background(), req)
	if res.Verdict != Block {
		t.Fatalf("verdict = %v, want Block via InputMessages fallback", res.Verdict)
	}
}

func TestRegexBlocklistPreCallAllowWithMessages(t *testing.T) {
	g := NewRegexBlocklist("test", []string{`badword`}, nil)
	req := &provider.ResponseRequest{
		Messages: []provider.Message{{
			Role:    "user",
			Content: []provider.ContentBlock{{Type: "text", Text: "clean message"}},
		}},
	}
	res := g.PreCall(context.Background(), req)
	if res.Verdict != Allow {
		t.Fatalf("verdict = %v, want Allow", res.Verdict)
	}
}
