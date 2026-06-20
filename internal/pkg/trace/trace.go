package trace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"
)

// SpanContext holds lightweight trace/span identifiers for log-based tracing.
type SpanContext struct {
	TraceID  string
	SpanID   string
	ParentID string
	Name     string
	Started  time.Time
}

type spanKey struct{}

// StartSpan creates a new child span attached to ctx.
// If a parent span exists in ctx, its SpanID becomes the new span's ParentID.
func StartSpan(ctx context.Context, traceID, name string) context.Context {
	parentID := ""
	if parent, ok := ctx.Value(spanKey{}).(*SpanContext); ok && parent != nil {
		parentID = parent.SpanID
	}
	span := &SpanContext{
		TraceID:  traceID,
		SpanID:   generateID(8),
		ParentID: parentID,
		Name:     name,
		Started:  time.Now(),
	}
	return context.WithValue(ctx, spanKey{}, span)
}

// FinishSpan logs the span completion with duration and optional tags.
func FinishSpan(ctx context.Context, tags ...map[string]string) {
	span, ok := ctx.Value(spanKey{}).(*SpanContext)
	if !ok || span == nil {
		return
	}
	duration := time.Since(span.Started)
	attrs := []any{
		"trace_id", span.TraceID,
		"span_id", span.SpanID,
		"parent_id", span.ParentID,
		"span_name", span.Name,
		"duration_ms", duration.Milliseconds(),
	}
	for _, t := range tags {
		for k, v := range t {
			attrs = append(attrs, k, v)
		}
	}
	slog.Info("span finished", attrs...)
}

// SpanFromContext returns the current span from ctx, if any.
func SpanFromContext(ctx context.Context) (*SpanContext, bool) {
	span, ok := ctx.Value(spanKey{}).(*SpanContext)
	return span, ok
}

func generateID(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", size*2)
	}
	return hex.EncodeToString(buf)
}
