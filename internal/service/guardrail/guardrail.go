// Package guardrail provides a pluggable interface for pre-/post-call
// validation of LLM requests and responses.
//
// Deprecated: Guardrail plugins are deprecated. New plugins should use the
// GatewayPlugin interface with pre_upstream / post_upstream phases instead.
// This package is kept for backward compatibility and will be removed in a
// future release.
//
// A Guardrail inspects either the inbound request (PreCall) or the
// upstream response (PostCall) and returns a Verdict that the gateway
// then enforces:
//
//   - Allow:     pass through unchanged
//   - Block:     reject with the supplied reason
//   - Transform: continue with the rewritten payload returned alongside
//
// Guardrails are intentionally narrow — they don't talk to providers,
// don't issue HTTP, don't see auth state. They just look at content and
// emit a verdict. That keeps them composable and easy to test.
//
// Patterned after Portkey / Kong AI-Prompt-Guard, but minimal: ship one
// reference implementation (regex blocklist) and let users write more
// when they need them.
package guardrail

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"

	"github.com/gateyes/gateway/internal/service/provider"
)

// Verdict is the outcome of a Guardrail invocation.
type Verdict int

const (
	// Allow lets the request/response continue unchanged.
	Allow Verdict = iota
	// Block aborts processing. The gateway returns a 400-class error to
	// the client (or terminates the stream with the supplied reason).
	Block
	// Transform replaces the in-flight payload with the rewritten one.
	// Use sparingly: it surprises clients that didn't ask for rewrites.
	Transform
)

func (v Verdict) String() string {
	switch v {
	case Allow:
		return "allow"
	case Block:
		return "block"
	case Transform:
		return "transform"
	default:
		return "unknown"
	}
}

// ErrBlocked is returned by Manager when any guardrail vetoes the
// request. Errors wrapping ErrBlocked may be checked with errors.Is.
var ErrBlocked = errors.New("guardrail blocked request")

// PreResult is the outcome of running a guardrail's PreCall hook.
// When verdict is Transform, Request points to the rewritten request.
// When verdict is Block, Reason is human-readable for clients/logs.
type PreResult struct {
	Verdict Verdict
	Request *provider.ResponseRequest
	Reason  string
}

// PostResult mirrors PreResult for the response side.
type PostResult struct {
	Verdict  Verdict
	Response *provider.Response
	Reason   string
}

// Guardrail is the contract for a single rule. Implementations must be
// safe for concurrent use and side-effect free beyond their own state.
type Guardrail interface {
	Name() string
	PreCall(ctx context.Context, req *provider.ResponseRequest) PreResult
	PostCall(ctx context.Context, resp *provider.Response) PostResult
}

// Manager runs a chain of guardrails in registration order. The first
// non-Allow verdict wins. Allow chains continue.
type Manager struct {
	chain []Guardrail
}

// New constructs a Manager from the given chain. nil and empty are valid
// (no-op manager).
func New(chain []Guardrail) *Manager {
	return &Manager{chain: append([]Guardrail(nil), chain...)}
}

// PreCall runs each guardrail's PreCall in order. Returns the first
// Block, or Transform if any guardrail rewrote the request, or Allow.
func (m *Manager) PreCall(ctx context.Context, req *provider.ResponseRequest) PreResult {
	if m == nil || len(m.chain) == 0 || req == nil {
		return PreResult{Verdict: Allow, Request: req}
	}
	current := req
	transformed := false
	for _, g := range m.chain {
		res := g.PreCall(ctx, current)
		switch res.Verdict {
		case Block:
			if res.Reason == "" {
				res.Reason = g.Name() + " blocked the request"
			}
			return res
		case Transform:
			if res.Request != nil {
				current = res.Request
				transformed = true
			}
		}
	}
	if transformed {
		return PreResult{Verdict: Transform, Request: current}
	}
	return PreResult{Verdict: Allow, Request: current}
}

// PostCall mirrors PreCall for responses.
func (m *Manager) PostCall(ctx context.Context, resp *provider.Response) PostResult {
	if m == nil || len(m.chain) == 0 || resp == nil {
		return PostResult{Verdict: Allow, Response: resp}
	}
	current := resp
	transformed := false
	for _, g := range m.chain {
		res := g.PostCall(ctx, current)
		switch res.Verdict {
		case Block:
			if res.Reason == "" {
				res.Reason = g.Name() + " blocked the response"
			}
			return res
		case Transform:
			if res.Response != nil {
				current = res.Response
				transformed = true
			}
		}
	}
	if transformed {
		return PostResult{Verdict: Transform, Response: current}
	}
	return PostResult{Verdict: Allow, Response: current}
}

// Close releases resources held by guardrails that implement io.Closer.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	var firstErr error
	for _, g := range m.chain {
		if c, ok := g.(io.Closer); ok {
			if err := c.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// RegexBlocklist is a reference Guardrail: blocks any request whose
// prompt contains a configured pattern, and (optionally) any response
// whose output text contains a pattern. Used for things like banned
// keyword lists, naive PII patterns, jailbreak markers.
//
// For real-world prompt injection detection, swap this for a model-based
// guardrail (Lakera, Aporia, your own classifier) that implements the
// same interface.
type RegexBlocklist struct {
	name       string
	requestRe  []*regexp.Regexp
	responseRe []*regexp.Regexp
}

// NewRegexBlocklist compiles the supplied patterns. A pattern that fails
// to compile is silently skipped (the gateway logs in the constructing
// caller — keep guardrail loading fail-open so a typo doesn't down the
// gateway).
func NewRegexBlocklist(name string, requestPatterns, responsePatterns []string) *RegexBlocklist {
	gb := &RegexBlocklist{name: name}
	for _, p := range requestPatterns {
		if re, err := regexp.Compile(p); err == nil {
			gb.requestRe = append(gb.requestRe, re)
		}
	}
	for _, p := range responsePatterns {
		if re, err := regexp.Compile(p); err == nil {
			gb.responseRe = append(gb.responseRe, re)
		}
	}
	return gb
}

func (g *RegexBlocklist) Name() string { return g.name }

func (g *RegexBlocklist) PreCall(_ context.Context, req *provider.ResponseRequest) PreResult {
	if req == nil || len(g.requestRe) == 0 {
		return PreResult{Verdict: Allow, Request: req}
	}
	body := req.InputText()
	if body == "" {
		// Fallback: concatenate text content blocks across messages.
		var b strings.Builder
		for _, m := range req.InputMessages() {
			for _, c := range m.Content {
				if c.Text != "" {
					b.WriteString(c.Text)
					b.WriteByte('\n')
				}
			}
		}
		body = b.String()
	}
	for _, re := range g.requestRe {
		if re.MatchString(body) {
			return PreResult{
				Verdict: Block,
				Reason:  g.name + ": request matched blocklist /" + re.String() + "/",
			}
		}
	}
	return PreResult{Verdict: Allow, Request: req}
}

func (g *RegexBlocklist) PostCall(_ context.Context, resp *provider.Response) PostResult {
	if resp == nil || len(g.responseRe) == 0 {
		return PostResult{Verdict: Allow, Response: resp}
	}
	body := resp.OutputText()
	for _, re := range g.responseRe {
		if re.MatchString(body) {
			return PostResult{
				Verdict: Block,
				Reason:  g.name + ": response matched blocklist /" + re.String() + "/",
			}
		}
	}
	return PostResult{Verdict: Allow, Response: resp}
}
