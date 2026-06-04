package gateyes

import "unsafe"

func readByte(ptr int32) byte {
	return *(*byte)(unsafe.Pointer(uintptr(ptr)))
}

func writeByte(ptr int32, val byte) {
	*(*byte)(unsafe.Pointer(uintptr(ptr))) = val
}

// readMemory reads a slice from the WASM linear memory at the given pointer.
func readMemory(ptr int32, length int32) []byte {
	result := make([]byte, length)
	for i := int32(0); i < length; i++ {
		result[i] = readByte(ptr + i)
	}
	return result
}

// writeMemory writes data to the WASM linear memory at the given pointer.
func writeMemory(ptr int32, data []byte) {
	for i, b := range data {
		writeByte(ptr+int32(i), b)
	}
}
