package responses

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/service/provider"
)

// TestDrainStreamForUsageCapturesFinalUsageChunk verifies the helper that
// runs after a client disconnect — it must continue draining the upstream
// stream long enough to capture the EventContentDelta that carries usage.
//
// Provider-style scenario: content delta first (no usage), then a final
// chunk with the usage struct, then channel close. The client has already
// gone away, but we still want the usage for billing.
func TestDrainStreamForUsageCapturesFinalUsageChunk(t *testing.T) {
	stream := make(chan provider.ResponseEvent, 4)
	upstreamErrCh := make(chan error)

	stream <- provider.ResponseEvent{
		Type:  provider.EventContentDelta,
		Delta: "partial",
	}
	stream <- provider.ResponseEvent{
		Type:  provider.EventContentDelta,
		Usage: &provider.Usage{PromptTokens: 5, CompletionTokens: 7, TotalTokens: 12},
	}
	stream <- provider.ResponseEvent{
		Type: provider.EventResponseCompleted,
		Response: &provider.Response{
			ID:    "r1",
			Model: "m1",
			Usage: provider.Usage{PromptTokens: 5, CompletionTokens: 7, TotalTokens: 12},
		},
	}
	close(stream)

	svc := &Service{}
	var (
		finalResp     *provider.Response
		streamUsage   *provider.Usage
		streamOutputs []provider.ResponseOutput
		text          string
	)

	done := make(chan struct{})
	go func() {
		svc.drainStreamForUsage(stream, upstreamErrCh, &finalResp, &streamUsage, &streamOutputs, &text)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("drain did not exit within 2s")
	}

	if streamUsage == nil {
		t.Fatal("streamUsage = nil, expected to capture final usage chunk")
	}
	if streamUsage.TotalTokens != 12 || streamUsage.PromptTokens != 5 || streamUsage.CompletionTokens != 7 {
		t.Fatalf("streamUsage = %+v, want PromptTokens=5 CompletionTokens=7 TotalTokens=12", streamUsage)
	}
	if finalResp == nil {
		t.Fatal("finalResp = nil, expected EventResponseCompleted to populate it")
	}
	if !strings.Contains(text, "partial") {
		t.Fatalf("assistantText = %q, expected to contain accumulated delta", text)
	}
}

// TestDrainStreamForUsageReturnsOnUpstreamError ensures drain exits promptly
// when upstream signals failure rather than waiting for the full timeout.
func TestDrainStreamForUsageReturnsOnUpstreamError(t *testing.T) {
	stream := make(chan provider.ResponseEvent)
	upstreamErrCh := make(chan error, 1)
	upstreamErrCh <- errors.New("upstream broke")

	svc := &Service{}
	var (
		finalResp     *provider.Response
		streamUsage   *provider.Usage
		streamOutputs []provider.ResponseOutput
		text          string
	)

	start := time.Now()
	done := make(chan struct{})
	go func() {
		svc.drainStreamForUsage(stream, upstreamErrCh, &finalResp, &streamUsage, &streamOutputs, &text)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("drain did not return on upstream error within 1s")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("drain took %v on upstream error, want <100ms", elapsed)
	}
}

// TestDrainStreamForUsageExitsOnQuietPeriod ensures we don't wait the full
// absolute timeout when the upstream stops emitting events. This keeps the
// cancellation path responsive when providers have already flushed.
func TestDrainStreamForUsageExitsOnQuietPeriod(t *testing.T) {
	stream := make(chan provider.ResponseEvent, 1)
	upstreamErrCh := make(chan error)

	stream <- provider.ResponseEvent{Type: provider.EventContentDelta, Delta: "hi"}
	// Don't close; just leave silent to trigger quiet-period exit.

	svc := &Service{}
	var (
		finalResp     *provider.Response
		streamUsage   *provider.Usage
		streamOutputs []provider.ResponseOutput
		text          string
	)

	start := time.Now()
	done := make(chan struct{})
	go func() {
		svc.drainStreamForUsage(stream, upstreamErrCh, &finalResp, &streamUsage, &streamOutputs, &text)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("drain did not exit on quiet period within 2s")
	}
	elapsed := time.Since(start)
	if elapsed > streamCancelDrainQuiet+200*time.Millisecond {
		t.Fatalf("drain took %v on quiet period, want close to %v", elapsed, streamCancelDrainQuiet)
	}
	if !strings.Contains(text, "hi") {
		t.Fatalf("assistantText = %q, expected to contain delta read before quiet period", text)
	}
}
