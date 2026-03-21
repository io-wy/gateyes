package streaming

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newStreamingContext(t *testing.T, body io.Reader, writer http.ResponseWriter) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	rec, ok := writer.(*httptest.ResponseRecorder)
	if !ok {
		rec = nil
	}
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/", body)
	return c, rec
}

func TestNewStreamingCountTokensAndSendFinal(t *testing.T) {
	s := NewStreaming()
	if s == nil {
		t.Fatal("NewStreaming() = nil, want non-nil")
	}
	if got, want := s.countTokens("12345678"), 2; got != want {
		t.Fatalf("countTokens(12345678) = %d, want %d", got, want)
	}
	if got := s.countTokens("[DONE]"); got != 0 {
		t.Fatalf("countTokens([DONE]) = %d, want %d", got, 0)
	}

	rec := httptest.NewRecorder()
	c, _ := newStreamingContext(t, nil, rec)
	s.sendFinal(c, rec, 7)
	body := rec.Body.String()
	if !strings.Contains(body, `"completion_tokens":7`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("sendFinal() body = %q, want final usage chunk and done marker", body)
	}
}

func TestProxyChatStreamsChunksAndDone(t *testing.T) {
	s := NewStreaming()
	rec := httptest.NewRecorder()
	c, _ := newStreamingContext(t, nil, rec)

	stream := make(chan string, 2)
	errCh := make(chan error, 1)
	stream <- `{"delta":"hello"}`
	close(stream)

	s.ProxyChat(c, stream, errCh)

	body := rec.Body.String()
	if !strings.Contains(body, `data: {"delta":"hello"}`) {
		t.Fatalf("ProxyChat() body = %q, want forwarded chunk", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("ProxyChat() body = %q, want done marker", body)
	}
}

func TestProxyChatHandlesError(t *testing.T) {
	s := NewStreaming()
	rec := httptest.NewRecorder()
	c, _ := newStreamingContext(t, nil, rec)
	stream := make(chan string)
	errCh := make(chan error, 1)
	errCh <- errors.New("boom")

	s.ProxyChat(c, stream, errCh)
	if !strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("ProxyChat(error) body = %q, want %q", rec.Body.String(), "boom")
	}
}

func TestProxyCompletionForwardsUpstreamJSON(t *testing.T) {
	s := NewStreaming()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`{"delta":"hello"}` + "\n" + `{"done":true}`))
	}))
	defer upstream.Close()

	rec := httptest.NewRecorder()
	c, _ := newStreamingContext(t, bytes.NewBufferString(`{"input":"hello"}`), rec)
	c.Request.Header.Set("Authorization", "Bearer test")

	if err := s.ProxyCompletion(c, upstream.URL, bytes.NewBufferString(`{"input":"hello"}`)); err != nil {
		t.Fatalf("ProxyCompletion() error: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data: {"delta":"hello"}`) {
		t.Fatalf("ProxyCompletion() body = %q, want forwarded upstream chunk", body)
	}
	if !strings.Contains(body, `data: {"done":true}`) {
		t.Fatalf("ProxyCompletion() body = %q, want final done chunk", body)
	}
}
