// wasm_auditor is a sample gateyes WASM gateway plugin that logs all
// request lifecycle events to stdout.
//
// Build:
//
//	tinygo build -o wasm_auditor.wasm -target=wasi -no-debug -opt=z .
package main

import (
	"fmt"

	"github.com/gateyes/gateway/plugins/sdk/gateyes"
)

//export evaluate_gateway
func evaluateGateway(inputPtr, inputLen, outputPtr, outputMaxLen int32) int32 {
	ev := gateyes.ReadGatewayEvent(inputPtr, inputLen)
	ctx := ev.Context

	switch ev.Phase {
	case "pre_upstream":
		fmt.Printf("[WASM_AUDIT] pre_upstream | trace=%s tenant=%s model=%s | payload_size=%d\n",
			ctx.TraceID, ctx.TenantID, ctx.Model, len(ev.Payload))
	case "post_upstream":
		fmt.Printf("[WASM_AUDIT] post_upstream | trace=%s tenant=%s model=%s | payload_size=%d\n",
			ctx.TraceID, ctx.TenantID, ctx.Model, len(ev.Payload))
	case "audit":
		fmt.Printf("[WASM_AUDIT] audit | trace=%s tenant=%s model=%s | payload_size=%d\n",
			ctx.TraceID, ctx.TenantID, ctx.Model, len(ev.Payload))
	default:
		fmt.Printf("[WASM_AUDIT] unhandled phase: %s\n", ev.Phase)
	}

	return gateyes.WriteGatewayCommand(outputPtr, gateyes.AllowGateway())
}

func main() {}
