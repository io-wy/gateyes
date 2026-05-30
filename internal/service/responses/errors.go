package responses

import (
	"errors"
	"fmt"

	"github.com/gateyes/gateway/internal/service/auth"
)

var (
	ErrNoProvider         = errors.New("no provider available")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
	ErrOutputBudgetTooLow = errors.New("output budget too low")
)

// ErrGuardrailBlocked is returned when a guardrail vetoes the request
// or response. The wrapped error includes the guardrail name + reason.
var ErrGuardrailBlocked = errors.New("blocked by guardrail")

func WrapError(err error) ginError {
	switch {
	case errors.Is(err, auth.ErrModelNotAllowed):
		return ginError{Status: 403, Message: err.Error(), Type: "invalid_request_error"}
	case errors.Is(err, auth.ErrQuotaExceeded):
		return ginError{Status: 429, Message: err.Error(), Type: "rate_limit_error"}
	case errors.Is(err, auth.ErrBudgetExceeded):
		return ginError{Status: 429, Message: err.Error(), Type: "rate_limit_error"}
	case errors.Is(err, ErrOutputBudgetTooLow):
		return ginError{Status: 400, Message: err.Error(), Type: "invalid_request_error"}
	case errors.Is(err, ErrNoProvider):
		return ginError{Status: 503, Message: err.Error(), Type: "internal_error"}
	default:
		return ginError{Status: 500, Message: err.Error(), Type: "internal_error"}
	}
}

type ginError struct {
	Status  int
	Message string
	Type    string
}

func (e ginError) Error() string {
	return fmt.Sprintf("%d %s", e.Status, e.Message)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
