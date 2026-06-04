package filter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const (
	wasmDefaultTimeout    = 50 * time.Millisecond
	wasmDefaultMemoryPages = 1  // 64KB per page
	wasmDefaultPoolSize    = 10
	wasmOutputBufSize      = 4096
)

// WASMMetricsRecorder is called after each WASM plugin invocation.
type WASMMetricsRecorder interface {
	RecordWASMExecution(name string, latency time.Duration, success bool)
}

// WASMFilterOptions configures a WASMFilter.
type WASMFilterOptions struct {
	// Timeout caps the WASM execution time. Zero uses the default (50ms).
	Timeout time.Duration

	// MemoryPages is the number of 64KB memory pages allocated to each
	// instance. Zero uses the default (1 page).
	MemoryPages uint32

	// PoolSize is the maximum number of WASM instances kept in the pool.
	// Zero uses the default (10).
	PoolSize int

	// Condition is called before every WASM invocation. If it returns false
	// the filter is skipped (treated as Allow). Nil means always run.
	Condition func(*RequestContext) bool

	// Metrics receives execution telemetry. Nil means no metrics.
	Metrics WASMMetricsRecorder
}

// WASMFilter runs a WebAssembly module as a request filter.
// The guest module must export:
//
//	evaluate(inputPtr i32, inputLen i32, outputPtr i32, outputMaxLen i32) i32
//
// and must have at least one page of memory exported as "memory".
//
// ABI contract:
//   - Host writes input JSON to [inputPtr, inputPtr+inputLen)
//   - Host calls evaluate
//   - Guest reads input, processes, writes result JSON to [outputPtr, outputPtr+outputMaxLen)
//   - Guest returns the length of the result JSON, or:
//     >0  → Host reads result JSON from outputPtr
//     0   → equivalent to Allow
//     <0  → error, treated as Block with 500
//
// WASMFilter is safe for concurrent use via an internal instance pool.
type WASMFilter struct {
	name      string
	runtime   wazero.Runtime
	code      wazero.CompiledModule
	pool      chan api.Module
	maxPool   int
	timeout   time.Duration
	memPages  uint32
	condition func(*RequestContext) bool
	metrics   WASMMetricsRecorder
}

// NewWASMFilter creates a filter from pre-compiled WASM bytes.
// wasmBytes must be a valid WASM module conforming to the ABI above.
func NewWASMFilter(name string, wasmBytes []byte) (*WASMFilter, error) {
	return NewWASMFilterWithOptions(name, wasmBytes, WASMFilterOptions{})
}

// NewWASMFilterWithOptions creates a filter with custom options.
func NewWASMFilterWithOptions(name string, wasmBytes []byte, opts WASMFilterOptions) (*WASMFilter, error) {
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	// WASI is required for TinyGo-compiled plugins.
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	code, err := r.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("compile wasm module: %w", err)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = wasmDefaultTimeout
	}
	memPages := opts.MemoryPages
	if memPages == 0 {
		memPages = wasmDefaultMemoryPages
	}
	poolSize := opts.PoolSize
	if poolSize <= 0 {
		poolSize = wasmDefaultPoolSize
	}

	f := &WASMFilter{
		name:      name,
		runtime:   r,
		code:      code,
		pool:      make(chan api.Module, poolSize),
		maxPool:   poolSize,
		timeout:   timeout,
		memPages:  memPages,
		condition: opts.Condition,
		metrics:   opts.Metrics,
	}

	// Pre-warm the pool with a few instances.
	for i := 0; i < 2 && i < poolSize; i++ {
		mod, err := f.newInstance(ctx)
		if err != nil {
			break
		}
		f.pool <- mod
	}

	return f, nil
}

func (f *WASMFilter) newInstance(ctx context.Context) (api.Module, error) {
	// Skip _start because TinyGo WASI programs exit after main() completes.
	// We only need the exported evaluate function.
	mod, err := f.runtime.InstantiateModule(ctx, f.code,
		wazero.NewModuleConfig().
			WithName(fmt.Sprintf("%s_%d", f.name, time.Now().UnixNano())).
			WithStartFunctions())
	if err != nil {
		return nil, err
	}
	return mod, nil
}

func (f *WASMFilter) Name() string { return f.name }

// Process executes the WASM plugin. On pool exhaustion or WASM failure it
// fail-opens (returns Allow) so a buggy plugin cannot bring down the gateway.
func (f *WASMFilter) Process(req *RequestContext) Result {
	start := time.Now()
	defer func() {
		if f.metrics != nil {
			f.metrics.RecordWASMExecution(f.name, time.Since(start), true)
		}
	}()

	// Condition check
	if f.condition != nil && !f.condition(req) {
		return Result{Action: Allow}
	}

	ctx, cancel := context.WithTimeout(req.Context, f.timeout)
	defer cancel()

	// Acquire instance from pool (non-blocking)
	var mod api.Module
	select {
	case mod = <-f.pool:
	default:
		// Pool empty: create a fresh instance.
		var err error
		mod, err = f.newInstance(ctx)
		if err != nil {
			f.recordFailure(start)
			return Result{Action: Allow}
		}
	}
	defer f.returnInstance(mod)

	// Serialize request to JSON
	inputJSON, err := json.Marshal(wasmRequest{
		Model:           req.Model,
		EstimatedTokens: req.EstimatedTokens,
		Stream:          req.Stream,
		Body:            string(req.Body),
	})
	if err != nil {
		return Result{Action: Allow}
	}

	mem := mod.Memory()
	if mem == nil {
		return Result{Action: Allow}
	}

	memSize := uint64(mem.Size())

	// Write input to WASM memory at offset 0
	if uint64(len(inputJSON)) > memSize {
		return Result{Action: Allow}
	}
	mem.Write(0, inputJSON)

	// Reserve output buffer right after input
	outputPtr := uint64(len(inputJSON))
	outputMaxLen := uint64(wasmOutputBufSize)
	if outputPtr+outputMaxLen > memSize {
		outputMaxLen = memSize - outputPtr
	}

	// Call evaluate
	evalFn := mod.ExportedFunction("evaluate")
	if evalFn == nil {
		return Result{Action: Allow}
	}

	raw, err := evalFn.Call(ctx,
		uint64(0),
		uint64(len(inputJSON)),
		outputPtr,
		outputMaxLen,
	)
	if err != nil {
		return Result{Action: Allow}
	}
	if len(raw) == 0 {
		return Result{Action: Allow}
	}

	resultLen := int32(raw[0])
	if resultLen < 0 {
		return Result{
			Action:     Block,
			Error:      fmt.Errorf("wasm plugin error: code %d", resultLen),
			HTTPStatus: 500,
			ErrorType:  "internal_error",
		}
	}
	if resultLen == 0 {
		return Result{Action: Allow}
	}

	// Read result JSON from WASM memory
	outputBytes, ok := mem.Read(uint32(outputPtr), uint32(resultLen))
	if !ok {
		return Result{Action: Allow}
	}

	var out wasmResult
	if err := json.Unmarshal(outputBytes, &out); err != nil {
		return Result{Action: Allow}
	}

	if out.Action == "allow" {
		return Result{Action: Allow}
	}

	status := out.HTTPStatus
	if status == 0 {
		status = 400
	}
	return Result{
		Action:        Block,
		Error:         fmt.Errorf("%s", out.Message),
		HTTPStatus:    status,
		ErrorType:     out.ErrorType,
		MetricsResult: out.MetricsResult,
		MetricsClass:  out.MetricsClass,
	}
}

func (f *WASMFilter) returnInstance(mod api.Module) {
	select {
	case f.pool <- mod:
	default:
		// Pool full: close the instance.
		mod.Close(context.Background())
	}
}

func (f *WASMFilter) recordFailure(start time.Time) {
	if f.metrics != nil {
		f.metrics.RecordWASMExecution(f.name, time.Since(start), false)
	}
}

// Close shuts down the WASM runtime and drains the instance pool.
func (f *WASMFilter) Close() {
	close(f.pool)
	for mod := range f.pool {
		mod.Close(context.Background())
	}
	f.runtime.Close(context.Background())
}

// wasmRequest is the JSON payload sent to the WASM guest.
type wasmRequest struct {
	Model           string `json:"model"`
	EstimatedTokens int    `json:"estimated_tokens"`
	Stream          bool   `json:"stream"`
	Body            string `json:"body"`
}

// wasmResult is the JSON payload expected from the WASM guest.
type wasmResult struct {
	Action        string `json:"action"`
	Message       string `json:"message"`
	HTTPStatus    int    `json:"http_status"`
	ErrorType     string `json:"error_type"`
	MetricsResult string `json:"metrics_result"`
	MetricsClass  string `json:"metrics_class"`
}
