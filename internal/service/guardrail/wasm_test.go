package guardrail

import (
	_ "embed"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/service/provider"
)

//go:embed testdata/allow.wasm
var wasmAllowBytes []byte

//go:embed testdata/block.wasm
var wasmBlockBytes []byte

//go:embed testdata/transform_request.wasm
var wasmTransformReqBytes []byte

//go:embed testdata/transform_response.wasm
var wasmTransformRespBytes []byte

//go:embed testdata/loop.wasm
var wasmLoopBytes []byte

func TestWASMGuardrail_PreCallAllow(t *testing.T) {
	g, err := NewWASMGuardrailFromBytes("allow", wasmAllowBytes, 0, 0)
	if err != nil {
		t.Fatalf("create wasm guardrail: %v", err)
	}
	gr := g.(Guardrail)

	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}
	res := gr.PreCall(t.Context(), req)
	if res.Verdict != Allow {
		t.Fatalf("expected Allow, got %v", res.Verdict)
	}
	if res.Request != req {
		t.Fatal("expected original request on Allow")
	}
}

func TestWASMGuardrail_PreCallBlock(t *testing.T) {
	g, err := NewWASMGuardrailFromBytes("block", wasmBlockBytes, 0, 0)
	if err != nil {
		t.Fatalf("create wasm guardrail: %v", err)
	}
	gr := g.(Guardrail)

	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}
	res := gr.PreCall(t.Context(), req)
	if res.Verdict != Block {
		t.Fatalf("expected Block, got %v", res.Verdict)
	}
	if res.Reason != "wasm-block" {
		t.Fatalf("expected reason 'wasm-block', got %q", res.Reason)
	}
}

func TestWASMGuardrail_PreCallTransform(t *testing.T) {
	g, err := NewWASMGuardrailFromBytes("transform-req", wasmTransformReqBytes, 0, 0)
	if err != nil {
		t.Fatalf("create wasm guardrail: %v", err)
	}
	gr := g.(Guardrail)

	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}
	res := gr.PreCall(t.Context(), req)
	if res.Verdict != Transform {
		t.Fatalf("expected Transform, got %v", res.Verdict)
	}
	if res.Request == nil {
		t.Fatal("expected transformed request")
	}
	if res.Request.Model != "transformed-model" {
		t.Fatalf("expected model 'transformed-model', got %q", res.Request.Model)
	}
}

func TestWASMGuardrail_PostCallTransform(t *testing.T) {
	g, err := NewWASMGuardrailFromBytes("transform-resp", wasmTransformRespBytes, 0, 0)
	if err != nil {
		t.Fatalf("create wasm guardrail: %v", err)
	}
	gr := g.(Guardrail)

	resp := &provider.Response{
		ID:     "orig",
		Object: "response",
		Model:  "m1",
		Output: []provider.ResponseOutput{
			{Type: "message", Content: []provider.ResponseContent{
				{Type: "output_text", Text: "original"},
			}},
		},
		Usage: provider.Usage{TotalTokens: 1},
	}
	res := gr.PostCall(t.Context(), resp)
	if res.Verdict != Transform {
		t.Fatalf("expected Transform, got %v", res.Verdict)
	}
	if res.Response == nil {
		t.Fatal("expected transformed response")
	}
	if res.Response.Model != "transformed-model" {
		t.Fatalf("expected model 'transformed-model', got %q", res.Response.Model)
	}
	if len(res.Response.Output) == 0 || res.Response.Output[0].Content[0].Text != "rewritten-output" {
		t.Fatalf("expected rewritten output text, got %+v", res.Response.Output)
	}
}

func TestWASMGuardrail_FailOpenOnMissingEvaluate(t *testing.T) {
	g, err := NewWASMGuardrailFromBytes("missing", missingEvaluateWASM(), 0, 0)
	if err != nil {
		t.Fatalf("create wasm guardrail: %v", err)
	}
	gr := g.(Guardrail)

	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}
	res := gr.PreCall(t.Context(), req)
	if res.Verdict != Allow {
		t.Fatalf("expected fail-open Allow, got %v", res.Verdict)
	}
}

func TestWASMGuardrail_FailOpenOnTimeout(t *testing.T) {
	g, err := NewWASMGuardrailFromBytes("loop", wasmLoopBytes, 10, 0)
	if err != nil {
		t.Fatalf("create wasm guardrail: %v", err)
	}
	gr := g.(Guardrail)

	start := time.Now()
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}
	res := gr.PreCall(t.Context(), req)
	elapsed := time.Since(start)

	if res.Verdict != Allow {
		t.Fatalf("expected fail-open Allow on timeout, got %v", res.Verdict)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("timeout took too long: %v", elapsed)
	}
}

func TestWASMGuardrail_ManagerChainWithWASM(t *testing.T) {
	allow, err := NewWASMGuardrailFromBytes("allow", wasmAllowBytes, 0, 0)
	if err != nil {
		t.Fatalf("create allow guardrail: %v", err)
	}
	block, err := NewWASMGuardrailFromBytes("block", wasmBlockBytes, 0, 0)
	if err != nil {
		t.Fatalf("create block guardrail: %v", err)
	}

	m := New([]Guardrail{allow, block})
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}
	res := m.PreCall(t.Context(), req)
	if res.Verdict != Block {
		t.Fatalf("expected Block at end of chain, got %v", res.Verdict)
	}
	if res.Reason != "wasm-block" {
		t.Fatalf("expected 'wasm-block', got %q", res.Reason)
	}
}

func TestWASMGuardrail_ManagerTransformPropagates(t *testing.T) {
	transform, err := NewWASMGuardrailFromBytes("transform-req", wasmTransformReqBytes, 0, 0)
	if err != nil {
		t.Fatalf("create transform guardrail: %v", err)
	}
	allow, err := NewWASMGuardrailFromBytes("allow", wasmAllowBytes, 0, 0)
	if err != nil {
		t.Fatalf("create allow guardrail: %v", err)
	}

	m := New([]Guardrail{transform, allow})
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}
	res := m.PreCall(t.Context(), req)
	if res.Verdict != Transform {
		t.Fatalf("expected Transform after transform, got %v", res.Verdict)
	}
	if res.Request == nil || res.Request.Model != "transformed-model" {
		t.Fatalf("expected transformed request to propagate, got %+v", res.Request)
	}
}

func TestWASMGuardrail_Close(t *testing.T) {
	g, err := NewWASMGuardrailFromBytes("allow", wasmAllowBytes, 0, 0)
	if err != nil {
		t.Fatalf("create wasm guardrail: %v", err)
	}
	if err := g.(interface{ Close() error }).Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestManager_CloseClosesWASMGuardrails(t *testing.T) {
	g, _ := NewWASMGuardrailFromBytes("allow", wasmAllowBytes, 0, 0)
	m := New([]Guardrail{g})
	if err := m.Close(); err != nil {
		t.Fatalf("manager close: %v", err)
	}
}

// missingEvaluateWASM returns a minimal valid WASM module that exports memory
// but does not export an evaluate function. Used to test fail-open behavior.
func missingEvaluateWASM() []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, // magic
		0x01, 0x00, 0x00, 0x00, // version
		// Memory section (5): 1 memory, min=0 max=1
		0x05, 0x03, 0x01, 0x00, 0x01,
		// Export section (7): 1 export — "memory" (kind=memory, idx=0)
		// payload = 0x01 0x06 mem...ory 0x02 0x00 = 10 bytes
		0x07, 0x0a, 0x01, 0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,
	}
}
