package middleware

import (
	"context"
	"log/slog"
	"testing"
)

type fakeHandler struct {
	enabled bool
	attrs   []slog.Attr
}

func (f *fakeHandler) Enabled(ctx context.Context, level slog.Level) bool { return f.enabled }
func (f *fakeHandler) Handle(ctx context.Context, r slog.Record) error    { return nil }
func (f *fakeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &fakeHandler{enabled: f.enabled, attrs: append(f.attrs, attrs...)}
}
func (f *fakeHandler) WithGroup(name string) slog.Handler { return f }

func TestTraceHandler_Enabled(t *testing.T) {
	base := &fakeHandler{enabled: true}
	th := NewTraceHandler(base)
	if !th.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("expected Enabled true")
	}

	base.enabled = false
	if th.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatal("expected Enabled false")
	}
}

func TestTraceHandler_WithAttrs(t *testing.T) {
	base := &fakeHandler{enabled: true}
	th := NewTraceHandler(base)
	newHandler := th.WithAttrs([]slog.Attr{slog.String("key", "value")})
	if newHandler == th {
		t.Fatal("expected new handler instance")
	}
	fh, ok := newHandler.(*TraceHandler)
	if !ok {
		t.Fatal("expected *TraceHandler")
	}
	inner, ok := fh.handler.(*fakeHandler)
	if !ok {
		t.Fatal("expected inner fakeHandler")
	}
	if len(inner.attrs) != 1 || inner.attrs[0].Key != "key" {
		t.Fatalf("unexpected attrs: %+v", inner.attrs)
	}
}
