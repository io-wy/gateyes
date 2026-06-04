package filter

import (
	"context"

	"github.com/gateyes/gateway/internal/repository"
)

// Action is the outcome of a filter evaluation.
type Action int

const (
	Allow Action = iota
	Block
)

// Result carries the outcome of a single filter evaluation.
type Result struct {
	Action        Action
	Error         error
	HTTPStatus    int
	ErrorType     string // OpenAI-compatible error type, e.g. "invalid_request_error"
	MetricsResult string // for MetricsRecorder classification
	MetricsClass  string // for MetricsRecorder error class
}

// RequestContext holds all information a filter needs to evaluate a request.
type RequestContext struct {
	Context         context.Context
	Identity        *repository.AuthIdentity
	Model           string
	EstimatedTokens int
	Stream          bool
	Body            []byte // raw request body for content-inspecting filters
}

// Filter is the core extension interface for request processing.
// Implementations must be safe for concurrent use and side-effect free
// beyond their own state.
type Filter interface {
	Name() string
	Process(req *RequestContext) Result
}
