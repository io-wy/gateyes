package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/gateyes/gateway/internal/domain/plugin"
)

const (
	wasmDefaultTimeout     = 50 * time.Millisecond
	wasmDefaultMemoryPages = 1
	wasmDefaultPoolSize    = 10
	wasmOutputBufSize      = 16384 // 16KB headroom for transformed payloads
)

// GatewayPlugin runs a WebAssembly module as a gateway plugin.
// It implements the plugin.Gateway interface.
//
// Guest ABI:
//
//	//export evaluate_gateway
//	func evaluateGateway(inputPtr i32, inputLen i32, outputPtr i32, outputMaxLen i32) i32
//
// Input JSON (host -> guest):
//
//	{"phase":"pre_upstream", "context":{"trace_id":"...","tenant_id":"..."}, "payload":{...}}
//
// Output JSON (guest -> host):
//
//	{"action":"ALLOW|BLOCK|TRANSFORM|CACHE_HIT|SKIP", "payload":..., "reason":"..."}
//
// Fail-open: any WASM error/timeout/bad JSON returns empty commands.
type GatewayPlugin struct {
	name     string
	runtime  wazero.Runtime
	code     wazero.CompiledModule
	pool     chan api.Module
	maxPool  int
	timeout  time.Duration
	memPages uint32
	phases   []string
	mu       sync.Mutex
}

// gatewayEnvelope is the JSON payload sent to the WASM guest.
type gatewayEnvelope struct {
	Phase   string          `json:"phase"`
	Context gatewayContext  `json:"context"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type gatewayContext struct {
	TraceID  string `json:"trace_id"`
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
}

// gatewayVerdict is the JSON payload expected from the WASM guest.
type gatewayVerdict struct {
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Reason  string          `json:"reason,omitempty"`
}

// NewGatewayPlugin creates a WASM-backed gateway plugin from a file path.
func NewGatewayPlugin(name, path string, phases []string, timeoutMs int, memoryPages uint32) (*GatewayPlugin, error) {
	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read wasm file %s: %w", path, err)
	}
	return NewGatewayPluginFromBytes(name, wasmBytes, phases, timeoutMs, memoryPages)
}

// NewGatewayPluginFromBytes creates a WASM-backed gateway plugin from bytes.
func NewGatewayPluginFromBytes(name string, wasmBytes []byte, phases []string, timeoutMs int, memoryPages uint32) (*GatewayPlugin, error) {
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

	g := &GatewayPlugin{
		name:     name,
		runtime:  r,
		code:     code,
		pool:     make(chan api.Module, wasmDefaultPoolSize),
		maxPool:  wasmDefaultPoolSize,
		timeout:  timeout,
		memPages: pages,
		phases:   phases,
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

func (g *GatewayPlugin) newInstance(ctx context.Context) (api.Module, error) {
	mod, err := g.runtime.InstantiateModule(ctx, g.code,
		wazero.NewModuleConfig().
			WithName(fmt.Sprintf("%s_%d", g.name, time.Now().UnixNano())).
			WithStartFunctions().
			WithStdout(os.Stdout).
			WithStderr(os.Stderr))
	if err != nil {
		return nil, err
	}
	return mod, nil
}

func (g *GatewayPlugin) Name() string { return g.name }
func (g *GatewayPlugin) Type() string { return "gateway" }

// Health always returns healthy for WASM plugins (no network health check).
func (g *GatewayPlugin) Health() plugin.HealthStatus { return plugin.HealthHealthy }

func (g *GatewayPlugin) Phases() []string { return g.phases }

func (g *GatewayPlugin) Process(ctx context.Context, phase plugin.Phase, payload []byte, traceID, tenantID, userID, model string, stream bool) ([]plugin.Command, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	var mod api.Module
	select {
	case mod = <-g.pool:
	default:
		var err error
		mod, err = g.newInstance(ctx)
		if err != nil {
			return nil, fmt.Errorf("new wasm instance: %w", err)
		}
	}
	defer g.returnInstance(mod)

	env := gatewayEnvelope{
		Phase: string(phase),
		Context: gatewayContext{
			TraceID:  traceID,
			TenantID: tenantID,
			UserID:   userID,
			Model:    model,
			Stream:   stream,
		},
		Payload: payload,
	}

	inputJSON, err := json.Marshal(env)
	if err != nil {
		g.closeInstance(mod)
		return nil, fmt.Errorf("marshal envelope: %w", err)
	}

	mem := mod.Memory()
	if mem == nil {
		g.closeInstance(mod)
		return nil, fmt.Errorf("no memory exported")
	}

	memSize := uint64(mem.Size())
	if uint64(len(inputJSON)) > memSize {
		g.closeInstance(mod)
		return nil, fmt.Errorf("input exceeds memory")
	}
	mem.Write(0, inputJSON)

	outputPtr := uint64(len(inputJSON))
	outputMaxLen := uint64(wasmOutputBufSize)
	if outputPtr+outputMaxLen > memSize {
		outputMaxLen = memSize - outputPtr
	}

	evalFn := mod.ExportedFunction("evaluate_gateway")
	if evalFn == nil {
		g.closeInstance(mod)
		return nil, fmt.Errorf("evaluate_gateway not exported")
	}

	raw, err := evalFn.Call(ctx, 0, uint64(len(inputJSON)), outputPtr, outputMaxLen)
	if err != nil {
		g.closeInstance(mod)
		return nil, fmt.Errorf("wasm call: %w", err)
	}
	if len(raw) == 0 {
		g.returnInstance(mod)
		return nil, nil
	}

	resultLen := int32(raw[0])
	if resultLen <= 0 {
		g.closeInstance(mod)
		return nil, fmt.Errorf("plugin returned error code %d", resultLen)
	}

	outputBytes, ok := mem.Read(uint32(outputPtr), uint32(resultLen))
	if !ok {
		g.closeInstance(mod)
		return nil, fmt.Errorf("read output failed")
	}

	var out gatewayVerdict
	if err := json.Unmarshal(outputBytes, &out); err != nil {
		g.closeInstance(mod)
		return nil, fmt.Errorf("unmarshal verdict: %w", err)
	}

	g.returnInstance(mod)

	// WASM SDK serializes []byte fields as base64 strings. Try to decode
	// a base64-encoded payload so TRANSFORM/CACHE_HIT commands carry raw JSON.
	pluginPayload := out.Payload
	if len(pluginPayload) > 0 {
		var decoded []byte
		if err = json.Unmarshal(pluginPayload, &decoded); err == nil {
			pluginPayload = decoded
		}
	}

	cmd := plugin.Command{
		Action:  out.Action,
		Payload: pluginPayload,
		Reason:  out.Reason,
	}
	return []plugin.Command{cmd}, nil
}

func (g *GatewayPlugin) returnInstance(mod api.Module) {
	select {
	case g.pool <- mod:
	default:
		g.closeInstance(mod)
	}
}

func (g *GatewayPlugin) closeInstance(mod api.Module) {
	mod.Close(context.Background())
}

// Close shuts down the WASM runtime and drains the instance pool.
func (g *GatewayPlugin) Close() error {
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
