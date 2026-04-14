package gateerror

import (
	"errors"
	"strings"
)

type Code string

const (
	CodeUnknown   Code = "unknown"
	CodeConfig    Code = "config"
	CodeParse     Code = "parse"
	CodeTransport Code = "transport"
	CodeUpstream  Code = "upstream"
)

type Error struct {
	Op      string
	Code    Code
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}

	parts := make([]string, 0, 4)
	if e.Op != "" {
		parts = append(parts, e.Op)
	}
	if e.Code != "" && e.Code != CodeUnknown {
		parts = append(parts, string(e.Code))
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.Err != nil {
		parts = append(parts, e.Err.Error())
	}
	if len(parts) == 0 {
		return string(CodeUnknown)
	}
	return strings.Join(parts, ": ")
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func New(code Code, message string) *Error {
	return &Error{
		Code:    normalizeCode(code),
		Message: strings.TrimSpace(message),
	}
}

func Wrap(op string, code Code, err error, message string) *Error {
	return &Error{
		Op:      strings.TrimSpace(op),
		Code:    normalizeCode(code),
		Message: strings.TrimSpace(message),
		Err:     err,
	}
}

func CodeOf(err error) Code {
	var target *Error
	if errors.As(err, &target) && target != nil {
		return normalizeCode(target.Code)
	}
	return CodeUnknown
}

func IsCode(err error, code Code) bool {
	return CodeOf(err) == normalizeCode(code)
}

func normalizeCode(code Code) Code {
	if strings.TrimSpace(string(code)) == "" {
		return CodeUnknown
	}
	return code
}
