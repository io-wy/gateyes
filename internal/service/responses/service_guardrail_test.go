package responses

import (
	"context"
	"errors"
	"testing"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/guardrail"
	"github.com/gateyes/gateway/internal/service/provider"
)

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

	_, err := svc.Create(context.Background(), identity, req, "")
	if err == nil {
		t.Fatal("Create() returned nil error, expected guardrail block")
	}
	if !errors.Is(err, ErrGuardrailBlocked) {
		t.Fatalf("err = %v, want wraps ErrGuardrailBlocked", err)
	}
}

func TestCreatePreCallGuardrailAllowsBenignRequest(t *testing.T) {
	// Verify the guardrail itself does not block a benign prompt — service-
	// level integration with provider routing is covered by other tests.
	gr := guardrail.New([]guardrail.Guardrail{
		guardrail.NewRegexBlocklist("blocklist", []string{`forbidden`}, nil),
	})
	req := &provider.ResponseRequest{Model: "m1", Input: "hello there"}
	res := gr.PreCall(context.Background(), req)
	if res.Verdict != guardrail.Allow {
		t.Fatalf("benign verdict = %v, want Allow", res.Verdict)
	}
}
