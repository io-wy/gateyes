package gateyes

import "encoding/json"

// ReadRequest parses the input JSON from host memory into a Request.
func ReadRequest(inputPtr, inputLen int32) Request {
	data := readMemory(inputPtr, inputLen)
	var req Request
	// Best-effort parse; on failure return zero value.
	_ = json.Unmarshal(data, &req)
	return req
}

// WriteResult serializes the Result to JSON and writes it to host memory.
// Returns the number of bytes written.
func WriteResult(outputPtr int32, result Result) int32 {
	data, err := json.Marshal(result)
	if err != nil {
		return 0
	}
	writeMemory(outputPtr, data)
	return int32(len(data))
}
