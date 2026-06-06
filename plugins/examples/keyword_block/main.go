// keyword_block is a sample gateyes WASM plugin that blocks requests
// containing sensitive keywords.
//
// Deprecated: use the GatewayPlugin interface (evaluate_gateway ABI) instead.
// See plugins/examples/wasm_auditor for the recommended approach.
//
// Build:
//
//	tinygo build -o keyword_block.wasm -target=wasi -no-debug -opt=z .
package main

import (
	"strings"

	"github.com/gateyes/gateway/plugins/sdk/gateyes"
)

var blockedKeywords = []string{
	"credit_card",
	"ssn",
	"password",
}

//export evaluate
func evaluate(inputPtr, inputLen, outputPtr, outputMaxLen int32) int32 {
	req := gateyes.ReadRequest(inputPtr, inputLen)

	bodyLower := strings.ToLower(req.Body)
	for _, kw := range blockedKeywords {
		if strings.Contains(bodyLower, kw) {
			return gateyes.WriteResult(outputPtr, gateyes.Block(400,
				"request contains prohibited keyword: "+kw))
		}
	}

	return gateyes.WriteResult(outputPtr, gateyes.Allow())
}

func main() {}
