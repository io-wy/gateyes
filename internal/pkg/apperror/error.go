package apperror

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeInvalidArgument    Code = "invalid_argument"
	CodeNoAvailableChannel Code = "no_available_channel"
	CodeRateLimited        Code = "rate_limited"
	CodeInternal           Code = "internal"
)

type Error struct {
	Code    Code
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func (e *Error) Is(target error) bool {
	targetErr, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == targetErr.Code
}

func New(code Code, message string, cause error) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

func Wrap(code Code, err error) *Error {
	if err == nil {
		return nil
	}
	return &Error{
		Code:  code,
		Cause: err,
	}
}

func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	appErr := &Error{}
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return CodeInternal
}

func (e *Error) String() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return fmt.Sprintf("code=%s message=%s", e.Code, e.Message)
	}
	return fmt.Sprintf("code=%s message=%s cause=%v", e.Code, e.Message, e.Cause)
}

func Match(code Code) *Error {
	return &Error{Code: code}
}
