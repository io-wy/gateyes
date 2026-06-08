package responses

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/guardrail"
	"github.com/gateyes/gateway/internal/service/provider"
)

var wasmBlockBytes []byte

func init() {
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	path := filepath.Join(dir, "..", "guardrail", "testdata", "block.wasm")
	var err error
	wasmBlockBytes, err = os.ReadFile(path)
	if err != nil {
		panic("failed to read block.wasm: " + err.Error())
	}
}

func TestCreatePreCallGuardrailBlocksBeforeProvider(t *testing.T) {
	gr := guardrail.New([]guardrail.Guardrail{
		guardrail.NewRegexBlocklist("blocklist", []string{`evil`}, nil),
	})
	svc := New(&Dependencies{
		Config:     &config.Config{},
		Guardrails: gr,
	})

	identity := &repository.AuthIdentity{TenantID: "t1", UserID: "u1"}
	req := &provider.ResponseRequest{Model: "m1", Input: "do something evil please"}

	_, err := svc.Create(t.Context(), identity, req, "")
	if err == nil {
		t.Fatal("Create() returned nil error, expected guardrail block")
	}
	if !errors.Is(err, ErrGuardrailBlocked) {
		t.Fatalf("err = %v, want wraps ErrGuardrailBlocked", err)
	}
}

func TestCreatePreCallGuardrailAllowsBenignRequest(t *testing.T) {
	gr := guardrail.New([]guardrail.Guardrail{
		guardrail.NewRegexBlocklist("blocklist", []string{`forbidden`}, nil),
	})
	req := &provider.ResponseRequest{Model: "m1", Input: "hello there"}
	res := gr.PreCall(t.Context(), req)
	if res.Verdict != guardrail.Allow {
		t.Fatalf("benign verdict = %v, want Allow", res.Verdict)
	}
}

func TestCreatePreCallWASMGuardrailBlocksBeforeProvider(t *testing.T) {
	g, err := guardrail.NewWASMGuardrailFromBytes("wasm-block", wasmBlockBytes, 100, 1)
	if err != nil {
		t.Fatalf("create wasm guardrail: %v", err)
	}
	gr := guardrail.New([]guardrail.Guardrail{g})
	svc := New(&Dependencies{
		Config:     &config.Config{},
		Guardrails: gr,
	})

	identity := &repository.AuthIdentity{TenantID: "t1", UserID: "u1"}
	req := &provider.ResponseRequest{Model: "m1", Input: "hello"}

	_, err = svc.Create(t.Context(), identity, req, "")
	if err == nil {
		t.Fatal("Create() returned nil error, expected guardrail block")
	}
	if !errors.Is(err, ErrGuardrailBlocked) {
		t.Fatalf("err = %v, want wraps ErrGuardrailBlocked", err)
	}
}

func TestRunStreamPostChecksReturnsRespOnBlock(t *testing.T) {
	gr := guardrail.New([]guardrail.Guardrail{
		guardrail.NewRegexBlocklist("blocklist", nil, []string{`blocked`}),
	})
	svc := New(&Dependencies{
		Config:     &config.Config{},
		Guardrails: gr,
	})

	identity := &repository.AuthIdentity{TenantID: "t1", UserID: "u1"}
	req := &provider.ResponseRequest{Model: "m1"}
	resp := provider.NewTextResponse("r1", "m1", "this is blocked content", provider.Usage{})

	// Block should return the response (not nil) regardless of hasSentPayload.
	gotResp, gotErr := svc.runStreamPostChecks(t.Context(), identity, req, resp, "tid", "t1", "u1", "m1", true, true)
	if gotResp == nil {
		t.Fatal("runStreamPostChecks returned nil response on block, want original response")
	}
	if gotErr == nil {
		t.Fatal("runStreamPostChecks returned nil error, expected block error")
	}
	if !errors.Is(gotErr, ErrGuardrailBlocked) {
		t.Fatalf("err = %v, want wraps ErrGuardrailBlocked", gotErr)
	}

	// Same behavior when hasSentPayload is false.
	gotResp2, gotErr2 := svc.runStreamPostChecks(t.Context(), identity, req, resp, "tid", "t1", "u1", "m1", true, false)
	if gotResp2 == nil {
		t.Fatal("runStreamPostChecks returned nil response on block (hasSentPayload=false)")
	}
	if gotErr2 == nil {
		t.Fatal("runStreamPostChecks returned nil error (hasSentPayload=false)")
	}
}

func TestRunStreamPostChecksAllowsCleanResponse(t *testing.T) {
	gr := guardrail.New([]guardrail.Guardrail{
		guardrail.NewRegexBlocklist("blocklist", nil, []string{`blocked`}),
	})
	svc := New(&Dependencies{
		Config:     &config.Config{},
		Guardrails: gr,
	})

	identity := &repository.AuthIdentity{TenantID: "t1", UserID: "u1"}
	req := &provider.ResponseRequest{Model: "m1"}
	resp := provider.NewTextResponse("r1", "m1", "clean content", provider.Usage{})

	gotResp, gotErr := svc.runStreamPostChecks(t.Context(), identity, req, resp, "tid", "t1", "u1", "m1", true, false)
	if gotErr != nil {
		t.Fatalf("runStreamPostChecks returned error for clean response: %v", gotErr)
	}
	if gotResp == nil {
		t.Fatal("runStreamPostChecks returned nil response for clean response")
	}
	if gotResp.OutputText() != "clean content" {
		t.Fatalf("response text = %q, want clean content", gotResp.OutputText())
	}
}
