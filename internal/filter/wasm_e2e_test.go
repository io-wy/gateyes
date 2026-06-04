package filter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// keywordBlockWASMPath returns the path to the compiled keyword_block.wasm.
func keywordBlockWASMPath() string {
	// From internal/filter, go up to project root, then into plugins/examples.
	_, b, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(b), "..", "..")
	return filepath.Join(root, "plugins", "examples", "keyword_block", "keyword_block.wasm")
}

func TestKeywordBlockPlugin(t *testing.T) {
	path := keywordBlockWASMPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("keyword_block.wasm not found, run: cd plugins/examples/keyword_block && tinygo build -o keyword_block.wasm -target=wasi -no-debug -opt=z .")
	}

	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read wasm: %v", err)
	}

	f, err := NewWASMFilter("keyword_block", wasmBytes)
	if err != nil {
		t.Fatalf("create wasm filter: %v", err)
	}
	defer f.Close()

	// Test 1: request with blocked keyword → Block
	res := f.Process(&RequestContext{
		Context: context.Background(),
		Body:    []byte(`{"body":"my credit_card number is 1234"}`),
	})
	if res.Action != Block {
		t.Fatalf("expected Block for credit_card, got %v", res.Action)
	}
	if res.HTTPStatus != 400 {
		t.Fatalf("expected 400, got %d", res.HTTPStatus)
	}

	// Test 2: clean request → Allow
	res2 := f.Process(&RequestContext{
		Context: context.Background(),
		Body:    []byte(`{"prompt":"hello world"}`),
	})
	if res2.Action != Allow {
		t.Fatalf("expected Allow for clean request, got %v", res2.Action)
	}
}

func TestKeywordBlockPlugin_InPipeline(t *testing.T) {
	path := keywordBlockWASMPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("keyword_block.wasm not found")
	}

	wasmBytes, _ := os.ReadFile(path)
	f, _ := NewWASMFilter("keyword_block", wasmBytes)
	defer f.Close()

	// Build pipeline: keyword_block only
	p := NewPipeline([]Filter{f})

	// Blocked request
	res := p.Execute(&RequestContext{
		Context: context.Background(),
		Body:    []byte(`{"body":"my ssn is 123-45-6789"}`),
	})
	if res.Action != Block {
		t.Fatalf("expected Block, got %v", res.Action)
	}

	// Clean request
	res2 := p.Execute(&RequestContext{
		Context: context.Background(),
		Body:    []byte(`{"body":"hello"}`),
	})
	if res2.Action != Allow {
		t.Fatalf("expected Allow, got %v", res2.Action)
	}
}

func TestPluginManager_LoadKeywordBlock(t *testing.T) {
	path := keywordBlockWASMPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("keyword_block.wasm not found")
	}

	// Create a temp dir with the wasm file
	tmpDir := t.TempDir()
	wasmBytes, _ := os.ReadFile(path)
	_ = os.WriteFile(filepath.Join(tmpDir, "keyword_block.wasm"), wasmBytes, 0644)

	pm := NewPluginManager(tmpDir)
	if err := pm.LoadAll(); err != nil {
		t.Fatalf("load all: %v", err)
	}

	f, ok := pm.Get("keyword_block")
	if !ok {
		t.Fatal("expected keyword_block plugin to be loaded")
	}
	defer f.Close()

	res := f.Process(&RequestContext{
		Context: context.Background(),
		Body:    []byte(`{"body":"my password is secret"}`),
	})
	if res.Action != Block {
		t.Fatalf("expected Block, got %v", res.Action)
	}
}
