// pii_guard is a sample gateyes WASM guardrail plugin that redacts
// US Social Security number-like patterns from both requests and responses.
//
// Deprecated: use the GatewayPlugin interface (evaluate_gateway ABI) instead.
// See plugins/examples/wasm_auditor for the recommended approach.
//
// Uses plain string scanning instead of regexp because TinyGo's regexp
// package panics at runtime under the wasi target.
//
// Build:
//
//	tinygo build -o pii_guard.wasm -target=wasi -no-debug -opt=z .
package main

import (
	"strings"

	"github.com/gateyes/gateway/plugins/sdk/gateyes"
)

// hasSSN reports whether s contains a substring that looks like
// "123-45-6789" (exactly 3 digits, '-', 2 digits, '-', 4 digits).
func hasSSN(s string) bool {
	for i := 0; i+10 < len(s); i++ {
		if s[i+3] == '-' && s[i+6] == '-' {
			if isDigits(s[i:i+3]) && isDigits(s[i+4:i+6]) && isDigits(s[i+7:i+11]) {
				return true
			}
		}
	}
	return false
}

// redactSSN replaces every SSN-like pattern in s with "[REDACTED-SSN]".
func redactSSN(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if i+10 < len(s) && s[i+3] == '-' && s[i+6] == '-' {
			if isDigits(s[i:i+3]) && isDigits(s[i+4:i+6]) && isDigits(s[i+7:i+11]) {
				b.WriteString("[REDACTED-SSN]")
				i += 11
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

//export evaluate
func evaluate(inputPtr, inputLen, outputPtr, outputMaxLen int32) int32 {
	env := gateyes.ReadGuardrailRequest(inputPtr, inputLen)

	switch env.Phase {
	case "pre":
		body := env.Request.Input
		if body == "" {
			body = env.Request.Body
		}
		if hasSSN(body) {
			rewritten := env.Request
			rewritten.Input = redactSSN(body)
			return gateyes.WriteGuardrailResult(outputPtr, gateyes.TransformRequest(rewritten))
		}
		return gateyes.WriteGuardrailResult(outputPtr, gateyes.AllowGuardrail())

	case "post":
		if len(env.Response.Output) == 0 {
			return gateyes.WriteGuardrailResult(outputPtr, gateyes.AllowGuardrail())
		}
		rewritten := env.Response
		rewritten.Output = make([]gateyes.OutputMessage, len(env.Response.Output))
		copy(rewritten.Output, env.Response.Output)

		modified := false
		for i := range rewritten.Output {
			if len(rewritten.Output[i].Content) == 0 {
				continue
			}
			rewritten.Output[i].Content = make([]gateyes.ContentBlock, len(env.Response.Output[i].Content))
			copy(rewritten.Output[i].Content, env.Response.Output[i].Content)
			for j := range rewritten.Output[i].Content {
				if rewritten.Output[i].Content[j].Type == "output_text" {
					text := rewritten.Output[i].Content[j].Text
					if hasSSN(text) {
						rewritten.Output[i].Content[j].Text = redactSSN(text)
						modified = true
					}
				}
			}
		}
		if modified {
			return gateyes.WriteGuardrailResult(outputPtr, gateyes.TransformResponse(rewritten))
		}
		return gateyes.WriteGuardrailResult(outputPtr, gateyes.AllowGuardrail())

	default:
		return gateyes.WriteGuardrailResult(outputPtr, gateyes.AllowGuardrail())
	}
}

func main() {}
