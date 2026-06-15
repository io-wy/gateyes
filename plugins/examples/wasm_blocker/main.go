// wasm_blocker is a gateyes WASM gateway plugin that blocks all post_upstream events
// and includes a payload to test whether wasm plugin payloads are delivered.
//
// Build:
//
//	tinygo build -o wasm_blocker.wasm -target=wasi -no-debug -opt=z .
package main

import (
	"github.com/gateyes/gateway/plugins/sdk/gateyes"
)

//export evaluate_gateway
func evaluateGateway(inputPtr, inputLen, outputPtr, outputMaxLen int32) int32 {
	cmd := gateyes.GatewayCommand{
		Action:  "BLOCK",
		Payload: []byte("payload-from-wasm"),
		Reason:  "blocked by wasm test plugin",
	}
	return gateyes.WriteGatewayCommand(outputPtr, cmd)
}

func main() {}
