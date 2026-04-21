package provider

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestUpstreamErrorHelpersAndConstructors(t *testing.T) {
	var nilUpstream *UpstreamError
	if nilUpstream.Unwrap() != nil {
		t.Fatalf("(*UpstreamError)(nil).Unwrap() = %v, want nil", nilUpstream.Unwrap())
	}

	root := errors.New("root cause")
	err429 := &UpstreamError{StatusCode: http.StatusTooManyRequests, Message: "rate limited", Err: root}
	if !strings.Contains(err429.Error(), "429") || !strings.Contains(err429.Error(), "rate limited") {
		t.Fatalf("UpstreamError.Error() = %q, want status/message", err429.Error())
	}
	if !errors.Is(err429.Unwrap(), root) {
		t.Fatalf("UpstreamError.Unwrap() = %v, want wrapped error", err429.Unwrap())
	}
	if !err429.IsRetryable() || !err429.IsRateLimited() || err429.IsUpstream() {
		t.Fatalf("429 helpers = retryable=%v rate_limited=%v upstream=%v, want true/true/false", err429.IsRetryable(), err429.IsRateLimited(), err429.IsUpstream())
	}

	err500 := &UpstreamError{StatusCode: http.StatusBadGateway, Message: "gateway boom"}
	if !err500.IsRetryable() || !err500.IsUpstream() || err500.IsRateLimited() {
		t.Fatalf("502 helpers = retryable=%v upstream=%v rate_limited=%v, want true/true/false", err500.IsRetryable(), err500.IsUpstream(), err500.IsRateLimited())
	}

	err400 := &UpstreamError{StatusCode: http.StatusBadRequest, Message: "bad request"}
	if err400.IsRetryable() || err400.IsUpstream() {
		t.Fatalf("400 helpers = retryable=%v upstream=%v, want false/false", err400.IsRetryable(), err400.IsUpstream())
	}

	timeoutErr := &UpstreamError{StatusCode: 0, Message: "request timeout"}
	if !timeoutErr.IsTimeout() {
		t.Fatalf("IsTimeout() = false, want true")
	}

	nilRespErr := newUpstreamStatusError(nil)
	var upstreamNil *UpstreamError
	if !errors.As(nilRespErr, &upstreamNil) || upstreamNil.Message != "empty upstream response" {
		t.Fatalf("newUpstreamStatusError(nil) = %#v, want empty upstream response", nilRespErr)
	}

	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(strings.NewReader("upstream failed")),
	}
	statusErr := newUpstreamStatusError(resp)
	var upstreamStatus *UpstreamError
	if !errors.As(statusErr, &upstreamStatus) || upstreamStatus.StatusCode != http.StatusBadGateway || upstreamStatus.Message != "upstream failed" {
		t.Fatalf("newUpstreamStatusError(resp) = %#v, want status/body mapped", statusErr)
	}

	if transportErr := newProviderTransportError("provider.transport", errors.New("dial failed")); transportErr == nil {
		t.Fatal("newProviderTransportError() = nil, want wrapped error")
	}
	if parseErr := newProviderParseError("provider.parse", errors.New("bad json"), "decode failed"); parseErr == nil {
		t.Fatal("newProviderParseError() = nil, want wrapped error")
	}
}
