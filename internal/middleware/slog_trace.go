package middleware

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// TraceHandler wraps a slog.Handler to inject trace_id and span_id from context.
type TraceHandler struct {
	handler slog.Handler
}

func NewTraceHandler(h slog.Handler) *TraceHandler {
	return &TraceHandler{handler: h}
}

func (t *TraceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return t.handler.Enabled(ctx, level)
}

func (t *TraceHandler) Handle(ctx context.Context, r slog.Record) error {
	span := trace.SpanFromContext(ctx)
	if span != nil {
		spanCtx := span.SpanContext()
		if spanCtx.HasTraceID() {
			r.AddAttrs(slog.String("trace_id", spanCtx.TraceID().String()))
		}
		if spanCtx.HasSpanID() {
			r.AddAttrs(slog.String("span_id", spanCtx.SpanID().String()))
		}
	}
	return t.handler.Handle(ctx, r)
}

func (t *TraceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return NewTraceHandler(t.handler.WithAttrs(attrs))
}

func (t *TraceHandler) WithGroup(name string) slog.Handler {
	return NewTraceHandler(t.handler.WithGroup(name))
}
