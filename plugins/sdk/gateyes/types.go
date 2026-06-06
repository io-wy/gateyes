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
// Deprecated: use GatewayEvent instead. Filter plugins should migrate to GatewayPlugin.
type Request struct {
	Model           string `json:"model"`
	EstimatedTokens int    `json:"estimated_tokens"`
	Stream          bool   `json:"stream"`
	Body            string `json:"body"`
	Input           string `json:"input,omitempty"`
}

// Result is the JSON payload the plugin sends back to the host.
// Deprecated: use GatewayCommand instead. Filter plugins should migrate to GatewayPlugin.
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

// Phase indicates whether a guardrail plugin is being called on the request
// or the response.
// Deprecated: GatewayPlugin uses string phase names directly.
type Phase string

const (
	PhasePre  Phase = "pre"
	PhasePost Phase = "post"
)

// ContentBlock mirrors the host ContentBlock for guardrail plugins.
type ContentBlock struct {
	Type       string `json:"type"`
	Text       string `json:"text,omitempty"`
	Thinking   string `json:"thinking,omitempty"`
	Signature  string `json:"signature,omitempty"`
	Refusal    string `json:"refusal,omitempty"`
}

// OutputMessage mirrors the host ResponseOutput for guardrail plugins.
type OutputMessage struct {
	ID      string         `json:"id,omitempty"`
	Type    string         `json:"type"`
	Role    string         `json:"role,omitempty"`
	Status  string         `json:"status,omitempty"`
	Content []ContentBlock `json:"content,omitempty"`
	CallID  string         `json:"call_id,omitempty"`
	Name    string         `json:"name,omitempty"`
	Args    string         `json:"arguments,omitempty"`
}

// Usage mirrors the host Usage for guardrail plugins.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CachedTokens     int `json:"cached_tokens"`
}

// Response mirrors the host Response for guardrail plugins.
type Response struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Status  string          `json:"status,omitempty"`
	Output  []OutputMessage `json:"output"`
	Usage   Usage           `json:"usage"`
}

// GuardrailRequest is the JSON payload the host sends to guardrail plugins.
// Deprecated: use GatewayEvent instead. Guardrail plugins should migrate to GatewayPlugin.
type GuardrailRequest struct {
	Phase    string   `json:"phase"`
	Request  Request  `json:"request,omitempty"`
	Response Response `json:"response,omitempty"`
}

// GuardrailResult is the JSON payload guardrail plugins send back to the host.
// Deprecated: use GatewayCommand instead. Guardrail plugins should migrate to GatewayPlugin.
type GuardrailResult struct {
	Verdict  string   `json:"verdict"`            // "allow", "block", or "transform"
	Reason   string   `json:"reason,omitempty"`   // required when verdict is "block"
	Request  Request  `json:"request,omitempty"`  // required when verdict is "transform" on pre
	Response Response `json:"response,omitempty"` // required when verdict is "transform" on post
}

// AllowGuardrail returns a Result that allows the request/response through.
func AllowGuardrail() GuardrailResult {
	return GuardrailResult{Verdict: "allow"}
}

// BlockGuardrail returns a Result that blocks the request/response.
func BlockGuardrail(reason string) GuardrailResult {
	return GuardrailResult{Verdict: "block", Reason: reason}
}

// TransformRequest returns a Result that rewrites the inbound request.
func TransformRequest(req Request) GuardrailResult {
	return GuardrailResult{Verdict: "transform", Request: req}
}

// TransformResponse returns a Result that rewrites the outbound response.
func TransformResponse(resp Response) GuardrailResult {
	return GuardrailResult{Verdict: "transform", Response: resp}
}
