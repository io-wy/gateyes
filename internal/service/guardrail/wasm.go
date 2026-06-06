package guardrail

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/gateyes/gateway/internal/service/provider"
)

const (
	wasmDefaultTimeout     = 50 * time.Millisecond
	wasmDefaultMemoryPages = 1
	wasmDefaultPoolSize    = 10
	wasmOutputBufSize      = 16384 // 16KB headroom for transformed responses
)

// wasmGuardrail runs a WebAssembly module as a guardrail.
//
// Deprecated: use GatewayPlugin (wasmPlugins config) with pre_upstream / post_upstream
// phases instead. This implementation is kept for backward compatibility.
//
// It implements the Guardrail interface and supports Allow/Block/Transform
// verdicts for both PreCall and PostCall.
//
// Guest ABI:
//
//	//export evaluate
//	func evaluate(inputPtr i32, inputLen i32, outputPtr i32, outputMaxLen i32) i32
//
// Input JSON (host -> guest):
//
//	{"phase":"pre"|"post", "request":{...ResponseRequest...}, "response":{...Response...}}
//
// Output JSON (guest -> host):
//
//	{"verdict":"allow"|"block"|"transform", "reason":"...", "request":{...}, "response":{...}}
//
// Fail-open: any WASM error/timeout/bad JSON returns Allow so a broken plugin
// cannot bring down the gateway.
type wasmGuardrail struct {
	name     string
	runtime  wazero.Runtime
	code     wazero.CompiledModule
	pool     chan api.Module
	maxPool  int
	timeout  time.Duration
	memPages uint32
	mu       sync.Mutex
}

// wasmEnvelope is the JSON payload sent to the WASM guest.
type wasmEnvelope struct {
	Phase    string         `json:"phase"`
	Request  *wasmRequest   `json:"request,omitempty"`
	Response *provider.Response `json:"response,omitempty"`
}

// wasmRequest is a simplified request shape that the TinyGo SDK understands.
// It includes a Body field so plugins can read the request text without
// dealing with the full messages array structure.
type wasmRequest struct {
	Model  string `json:"model"`
	Body   string `json:"body"`
	Input  string `json:"input,omitempty"`
	Stream bool   `json:"stream,omitempty"`
}

// wasmVerdict is the JSON payload expected from the WASM guest.
type wasmVerdict struct {
	Verdict  string                   `json:"verdict"`
	Reason   string                   `json:"reason,omitempty"`
	Request  *provider.ResponseRequest `json:"request,omitempty"`
	Response *provider.Response        `json:"response,omitempty"`
}

// NewWASMGuardrail creates a WASM-backed guardrail from a file path.
func NewWASMGuardrail(name, path string, timeoutMs int, memoryPages uint32) (Guardrail, error) {
	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read wasm file %s: %w", path, err)
	}
	return NewWASMGuardrailFromBytes(name, wasmBytes, timeoutMs, memoryPages)
}

// NewWASMGuardrailFromBytes creates a WASM-backed guardrail from bytes.
func NewWASMGuardrailFromBytes(name string, wasmBytes []byte, timeoutMs int, memoryPages uint32) (Guardrail, error) {
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx,
		wazero.NewRuntimeConfig().
			WithCloseOnContextDone(true))
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	code, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("compile wasm module: %w", err)
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = wasmDefaultTimeout
	}
	pages := memoryPages
	if pages == 0 {
		pages = wasmDefaultMemoryPages
	}

	g := &wasmGuardrail{
		name:     name,
		runtime:  r,
		code:     code,
		pool:     make(chan api.Module, wasmDefaultPoolSize),
		maxPool:  wasmDefaultPoolSize,
		timeout:  timeout,
		memPages: pages,
	}

	for i := 0; i < 2 && i < wasmDefaultPoolSize; i++ {
		mod, err := g.newInstance(ctx)
		if err != nil {
			break
		}
		g.pool <- mod
	}

	return g, nil
}

func (g *wasmGuardrail) newInstance(ctx context.Context) (api.Module, error) {
	mod, err := g.runtime.InstantiateModule(ctx, g.code,
		wazero.NewModuleConfig().
			WithName(fmt.Sprintf("%s_%d", g.name, time.Now().UnixNano())).
			WithStartFunctions())
	if err != nil {
		return nil, err
	}
	return mod, nil
}

func (g *wasmGuardrail) Name() string { return g.name }

func (g *wasmGuardrail) PreCall(ctx context.Context, req *provider.ResponseRequest) PreResult {
	if req == nil {
		return PreResult{Verdict: Allow, Request: req}
	}
	text := extractRequestText(req)
	out := g.evaluate(ctx, wasmEnvelope{
		Phase: "pre",
		Request: &wasmRequest{
			Model:  req.Model,
			Body:   text,
			Input:  text,
			Stream: req.Stream,
		},
	})
	return g.toPreResult(out, req)
}

// extractRequestText extracts the full text content from a ResponseRequest
// for plugins that only need to inspect the request body.
func extractRequestText(req *provider.ResponseRequest) string {
	var parts []string
	for _, m := range req.InputMessages() {
		for _, c := range m.Content {
			switch c.Type {
			case "text", "output_text":
				parts = append(parts, c.Text)
			case "thinking":
				parts = append(parts, c.Thinking)
			case "refusal":
				parts = append(parts, c.Refusal)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func (g *wasmGuardrail) PostCall(ctx context.Context, resp *provider.Response) PostResult {
	if resp == nil {
		return PostResult{Verdict: Allow, Response: resp}
	}
	out := g.evaluate(ctx, wasmEnvelope{Phase: "post", Response: resp})
	return g.toPostResult(out, resp)
}

func (g *wasmGuardrail) evaluate(ctx context.Context, env wasmEnvelope) wasmVerdict {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	var mod api.Module
	select {
	case mod = <-g.pool:
	default:
		var err error
		mod, err = g.newInstance(ctx)
		if err != nil {
			return wasmVerdict{Verdict: "allow"}
		}
	}
	defer g.returnInstance(mod)

	inputJSON, err := json.Marshal(env)
	if err != nil {
		g.closeInstance(mod)
		return wasmVerdict{Verdict: "allow"}
	}

	mem := mod.Memory()
	if mem == nil {
		g.closeInstance(mod)
		return wasmVerdict{Verdict: "allow"}
	}

	memSize := uint64(mem.Size())
	if uint64(len(inputJSON)) > memSize {
		g.closeInstance(mod)
		return wasmVerdict{Verdict: "allow"}
	}
	mem.Write(0, inputJSON)

	outputPtr := uint64(len(inputJSON))
	outputMaxLen := uint64(wasmOutputBufSize)
	if outputPtr+outputMaxLen > memSize {
		outputMaxLen = memSize - outputPtr
	}

	evalFn := mod.ExportedFunction("evaluate")
	if evalFn == nil {
		g.closeInstance(mod)
		return wasmVerdict{Verdict: "allow"}
	}

	raw, err := evalFn.Call(ctx, 0, uint64(len(inputJSON)), outputPtr, outputMaxLen)
	if err != nil {
		g.closeInstance(mod)
		return wasmVerdict{Verdict: "allow"}
	}
	if len(raw) == 0 {
		g.returnInstance(mod)
		return wasmVerdict{Verdict: "allow"}
	}

	resultLen := int32(raw[0])
	if resultLen <= 0 {
		// Negative return is treated as plugin error; fail-open.
		g.closeInstance(mod)
		return wasmVerdict{Verdict: "allow"}
	}

	outputBytes, ok := mem.Read(uint32(outputPtr), uint32(resultLen))
	if !ok {
		g.closeInstance(mod)
		return wasmVerdict{Verdict: "allow"}
	}

	var out wasmVerdict
	if err := json.Unmarshal(outputBytes, &out); err != nil {
		g.closeInstance(mod)
		return wasmVerdict{Verdict: "allow"}
	}

	g.returnInstance(mod)
	return out
}

func (g *wasmGuardrail) toPreResult(out wasmVerdict, original *provider.ResponseRequest) PreResult {
	switch out.Verdict {
	case "block":
		return PreResult{Verdict: Block, Reason: out.Reason}
	case "transform":
		if out.Request != nil {
			return PreResult{Verdict: Transform, Request: out.Request}
		}
	}
	return PreResult{Verdict: Allow, Request: original}
}

func (g *wasmGuardrail) toPostResult(out wasmVerdict, original *provider.Response) PostResult {
	switch out.Verdict {
	case "block":
		return PostResult{Verdict: Block, Reason: out.Reason}
	case "transform":
		if out.Response != nil {
			return PostResult{Verdict: Transform, Response: out.Response}
		}
	}
	return PostResult{Verdict: Allow, Response: original}
}

func (g *wasmGuardrail) returnInstance(mod api.Module) {
	select {
	case g.pool <- mod:
	default:
		g.closeInstance(mod)
	}
}

func (g *wasmGuardrail) closeInstance(mod api.Module) {
	mod.Close(context.Background())
}

// Close shuts down the WASM runtime and drains the instance pool.
func (g *wasmGuardrail) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pool == nil {
		return nil
	}
	close(g.pool)
	for mod := range g.pool {
		g.closeInstance(mod)
	}
	g.pool = nil
	return g.runtime.Close(context.Background())
}
