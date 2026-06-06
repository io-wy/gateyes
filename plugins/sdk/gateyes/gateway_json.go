package gateyes

import "encoding/json"

// ReadGatewayEvent parses the gateway event from host memory.
func ReadGatewayEvent(inputPtr, inputLen int32) GatewayEvent {
	data := readMemory(inputPtr, inputLen)
	var ev GatewayEvent
	_ = json.Unmarshal(data, &ev)
	return ev
}

// WriteGatewayCommand serializes a command to JSON and writes it to host memory.
// Returns the number of bytes written.
func WriteGatewayCommand(outputPtr int32, cmd GatewayCommand) int32 {
	data, err := json.Marshal(cmd)
	if err != nil {
		return 0
	}
	writeMemory(outputPtr, data)
	return int32(len(data))
}
