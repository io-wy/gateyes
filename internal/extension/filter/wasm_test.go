package filter

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
)

// ── WASM binary helpers ──────────────────────────────────────────────

// encodeULEB128 writes an unsigned LEB128 value.
func encodeULEB128(v uint32) []byte {
	var buf bytes.Buffer
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		buf.WriteByte(b)
		if v == 0 {
			break
		}
	}
	return buf.Bytes()
}

// encodeSLEB128 writes a signed LEB128 value.
func encodeSLEB128(v int32) []byte {
	var buf bytes.Buffer
	more := true
	for more {
		b := byte(v & 0x7f)
		v >>= 7
		if (v == 0 && (b&0x40) == 0) || (v == -1 && (b&0x40) != 0) {
			more = false
		} else {
			b |= 0x80
		}
		buf.WriteByte(b)
	}
	return buf.Bytes()
}

// wasmSection creates a WASM section: id | size | payload.
func wasmSection(id byte, payload []byte) []byte {
	var buf bytes.Buffer
	buf.WriteByte(id)
	buf.Write(encodeULEB128(uint32(len(payload))))
	buf.Write(payload)
	return buf.Bytes()
}

// wasmModule builds a minimal WASM module from sections.
func wasmModule(sections ...[]byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("\x00asm")                         // magic
	binary.Write(&buf, binary.LittleEndian, uint32(1)) // version 1
	for _, s := range sections {
		buf.Write(s)
	}
	return buf.Bytes()
}

// ── Pre-built WASM test modules ──────────────────────────────────────

// alwaysAllowWASM is a module that exports evaluate(...) -> 0 (Allow).
var alwaysAllowWASM = wasmModule(
	// Type section: (i32, i32, i32, i32) -> i32
	wasmSection(1, []byte{
		0x01,                   // 1 type
		0x60,                   // func
		0x04,                   // 4 params
		0x7f, 0x7f, 0x7f, 0x7f, // i32 x 4
		0x01, 0x7f, // 1 result, i32
	}),
	// Function section
	wasmSection(3, []byte{0x01, 0x00}), // 1 function, type 0
	// Memory section: 1 page, no max
	wasmSection(5, []byte{0x01, 0x00, 0x01}),
	// Export section
	wasmSection(7, []byte{
		0x02, // 2 exports
		0x08, // name len
		'e', 'v', 'a', 'l', 'u', 'a', 't', 'e',
		0x00, 0x00, // func, index 0
		0x06, // name len
		'm', 'e', 'm', 'o', 'r', 'y',
		0x02, 0x00, // memory, index 0
	}),
	// Code section: i32.const 0, end
	wasmSection(10, []byte{
		0x01,       // 1 body
		0x04,       // body size = 4 (0 locals + 2 instr + 1 end)
		0x00,       // 0 locals
		0x41, 0x00, // i32.const 0
		0x0b, // end
	}),
)

// alwaysBlockWASM returns -1 which the host interprets as an error → Block(500).
var alwaysBlockWASM = wasmModule(
	wasmSection(1, []byte{
		0x01, 0x60, 0x04, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f,
	}),
	wasmSection(3, []byte{0x01, 0x00}),
	wasmSection(5, []byte{0x01, 0x00, 0x01}),
	wasmSection(7, []byte{
		0x02, 0x08,
		'e', 'v', 'a', 'l', 'u', 'a', 't', 'e',
		0x00, 0x00,
		0x06,
		'm', 'e', 'm', 'o', 'r', 'y',
		0x02, 0x00,
	}),
	// Code: i32.const -1, end
	wasmSection(10, []byte{
		0x01,       // 1 body
		0x04,       // body size = 4 (0 locals + 2 instr + 1 end)
		0x00,       // 0 locals
		0x41, 0x7f, // i32.const -1  (sLEB128: 0x7f)
		0x0b, // end
	}),
)

// ── Tests ────────────────────────────────────────────────────────────

func TestWASMFilter_AlwaysAllow(t *testing.T) {
	f, err := NewWASMFilter("test_allow", alwaysAllowWASM)
	if err != nil {
		t.Fatalf("create wasm filter: %v", err)
	}
	res := f.Process(&RequestContext{Context: context.Background()})
	if res.Action != Allow {
		t.Fatalf("expected Allow, got %v", res.Action)
	}
}

func TestWASMFilter_AlwaysBlock(t *testing.T) {
	f, err := NewWASMFilter("test_block", alwaysBlockWASM)
	if err != nil {
		t.Fatalf("create wasm filter: %v", err)
	}
	res := f.Process(&RequestContext{Context: context.Background()})
	if res.Action != Block {
		t.Fatalf("expected Block, got %v", res.Action)
	}
	if res.HTTPStatus != 500 {
		t.Fatalf("expected status 500 (error code), got %d", res.HTTPStatus)
	}
}

func TestWASMFilter_PipelineIntegration(t *testing.T) {
	allowFilter, _ := NewWASMFilter("wasm_allow", alwaysAllowWASM)
	blockFilter, _ := NewWASMFilter("wasm_block", alwaysBlockWASM)

	// Chain: allowFilter -> blockFilter → should Block
	p := NewPipeline([]Filter{allowFilter, blockFilter})
	res := p.Execute(&RequestContext{Context: context.Background()})
	if res.Action != Block {
		t.Fatalf("expected Block at end of pipeline, got %v", res.Action)
	}

	// Chain: allowFilter only → should Allow
	p2 := NewPipeline([]Filter{allowFilter})
	res2 := p2.Execute(&RequestContext{Context: context.Background()})
	if res2.Action != Allow {
		t.Fatalf("expected Allow, got %v", res2.Action)
	}
}

func TestWASMFilter_Registry(t *testing.T) {
	r := NewRegistry()
	r.MustRegister("wasm_allow", func() (Filter, error) {
		return NewWASMFilter("wasm_allow", alwaysAllowWASM)
	})

	p, err := r.BuildPipeline([]string{"wasm_allow"})
	if err != nil {
		t.Fatalf("build pipeline: %v", err)
	}
	res := p.Execute(&RequestContext{Context: context.Background()})
	if res.Action != Allow {
		t.Fatalf("expected Allow, got %v", res.Action)
	}
}
