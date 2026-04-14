package provider

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	gateerror "github.com/gateyes/gateway/utils/error"
)

// UpstreamError represents an error from the upstream provider.
type UpstreamError struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream error: %d %s", e.StatusCode, e.Message)
}

func (e *UpstreamError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *UpstreamError) IsRetryable() bool {
	if e.StatusCode >= 400 && e.StatusCode < 500 {
		return e.StatusCode == http.StatusTooManyRequests
	}
	return e.StatusCode >= http.StatusInternalServerError
}

func (e *UpstreamError) IsTimeout() bool {
	return e.StatusCode == 0 && strings.Contains(strings.ToLower(e.Message), "timeout")
}

func (e *UpstreamError) IsRateLimited() bool {
	return e.StatusCode == http.StatusTooManyRequests
}

func (e *UpstreamError) IsUpstream() bool {
	return e.StatusCode >= http.StatusInternalServerError
}

func newUpstreamError(statusCode int, message string) *UpstreamError {
	return &UpstreamError{
		StatusCode: statusCode,
		Message:    strings.TrimSpace(message),
	}
}

func newUpstreamStatusError(resp *http.Response) error {
	if resp == nil {
		return newUpstreamError(0, "empty upstream response")
	}
	payload, _ := io.ReadAll(resp.Body)
	return newUpstreamError(resp.StatusCode, string(payload))
}

func newProviderTransportError(op string, err error) error {
	return gateerror.Wrap(op, gateerror.CodeTransport, err, "")
}

func newProviderParseError(op string, err error, message string) error {
	return gateerror.Wrap(op, gateerror.CodeParse, err, message)
}

func newProviderConfigError(op string, message string) error {
	return gateerror.Wrap(op, gateerror.CodeConfig, nil, message)
}

func newProviderUpstreamMessageError(message string) error {
	return gateerror.New(gateerror.CodeUnknown, message)
}
