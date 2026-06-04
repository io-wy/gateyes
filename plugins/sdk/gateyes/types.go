// Package gateyes provides a TinyGo SDK for writing gateyes WASM plugins.
//
// A plugin is a TinyGo program that exports an evaluate function:
//
//	//export evaluate
//	func evaluate(inputPtr, inputLen, outputPtr, outputMaxLen int32) int32
//
// The SDK abstracts the raw memory operations so developers can focus on
// business logic.
package gateyes

// Request is the JSON payload the host sends to the plugin.
type Request struct {
	Model           string `json:"model"`
	EstimatedTokens int    `json:"estimated_tokens"`
	Stream          bool   `json:"stream"`
	Body            string `json:"body"`
}

// Result is the JSON payload the plugin sends back to the host.
type Result struct {
	Action        string `json:"action"`         // "allow" or "block"
	Message       string `json:"message"`        // human-readable reason
	HTTPStatus    int    `json:"http_status"`    // suggested HTTP status
	ErrorType     string `json:"error_type"`     // OpenAI-compatible error type
	MetricsResult string `json:"metrics_result"` // for MetricsRecorder
	MetricsClass  string `json:"metrics_class"`  // for MetricsRecorder
}

// Allow returns a Result that allows the request through.
func Allow() Result {
	return Result{Action: "allow"}
}

// Block returns a Result that blocks the request.
func Block(status int, message string) Result {
	return Result{
		Action:     "block",
		HTTPStatus: status,
		Message:    message,
		ErrorType:  "invalid_request_error",
	}
}

// RateLimit returns a Result that rate-limits the request.
func RateLimit(message string) Result {
	return Result{
		Action:     "block",
		HTTPStatus: 429,
		Message:    message,
		ErrorType:  "rate_limit_error",
	}
}
