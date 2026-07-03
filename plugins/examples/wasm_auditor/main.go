// wasm_auditor is a sample gateyes WASM gateway plugin that logs all
// request lifecycle events to stdout.
//
// Build:
//
//	tinygo build -o wasm_auditor.wasm -target=wasi -no-debug -opt=z .
package main

import (
	"log/slog"
	"os"

	"github.com/gateyes/gateway/plugins/sdk/gateyes"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

//export evaluate_gateway
func evaluateGateway(inputPtr, inputLen, outputPtr, outputMaxLen int32) int32 {
	ev := gateyes.ReadGatewayEvent(inputPtr, inputLen)
	ctx := ev.Context

	switch ev.Phase {
	case "pre_upstream":
		logger.Info("[WASM_AUDIT] pre_upstream", "trace", ctx.TraceID, "tenant", ctx.TenantID, "model", ctx.Model, "payload_size", len(ev.Payload))
	case "post_upstream":
		logger.Info("[WASM_AUDIT] post_upstream", "trace", ctx.TraceID, "tenant", ctx.TenantID, "model", ctx.Model, "payload_size", len(ev.Payload))
	case "audit":
		logger.Info("[WASM_AUDIT] audit", "trace", ctx.TraceID, "tenant", ctx.TenantID, "model", ctx.Model, "payload_size", len(ev.Payload))
	default:
		logger.Info("[WASM_AUDIT] unhandled phase", "phase", ev.Phase)
	}

	return gateyes.WriteGatewayCommand(outputPtr, gateyes.AllowGateway())
}

func main() {}
