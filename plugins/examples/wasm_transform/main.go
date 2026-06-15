// wasm_transform is a gateyes WASM gateway plugin that demonstrates
// real transformation of the upstream response in the post_upstream phase.
// It prepends "[WASM] " to the first assistant text content.
//
// Build:
//
//	tinygo build -o wasm_transform.wasm -target=wasi -no-debug -opt=z .
package main

import (
	"encoding/json"

	"github.com/gateyes/gateway/plugins/sdk/gateyes"
)

// transformResponse prepends a marker to the first assistant text content.
// It returns the transformed *inner* response object (not the envelope).
func transformResponse(payload []byte) []byte {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return payload
	}
	respBytes, ok := envelope["response"]
	if !ok {
		return payload
	}

	var resp map[string]any
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return payload
	}
	outputs, ok := resp["output"].([]any)
	if !ok || len(outputs) == 0 {
		return payload
	}
	output, ok := outputs[0].(map[string]any)
	if !ok {
		return payload
	}
	contents, ok := output["content"].([]any)
	if !ok || len(contents) == 0 {
		return payload
	}
	content, ok := contents[0].(map[string]any)
	if !ok {
		return payload
	}
	text, ok := content["text"].(string)
	if !ok {
		return payload
	}
	content["text"] = "[WASM] " + text

	out, err := json.Marshal(resp)
	if err != nil {
		return payload
	}
	return out
}

//export evaluate_gateway
func evaluateGateway(inputPtr, inputLen, outputPtr, outputMaxLen int32) int32 {
	ev := gateyes.ReadGatewayEvent(inputPtr, inputLen)

	switch ev.Phase {
	case "post_upstream":
		if len(ev.Payload) == 0 {
			return gateyes.WriteGatewayCommand(outputPtr, gateyes.BlockGateway("payload_len=0"))
		}
		transformed := transformResponse(ev.Payload)
		if len(transformed) == 0 {
			return gateyes.WriteGatewayCommand(outputPtr, gateyes.BlockGateway("transform_len=0"))
		}
		return gateyes.WriteGatewayCommand(outputPtr, gateyes.TransformGateway(transformed))
	default:
		return gateyes.WriteGatewayCommand(outputPtr, gateyes.AllowGateway())
	}
}

func main() {}
