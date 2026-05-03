package trace

import (
	"context"
	"testing"
	"time"
)

func TestStartSpan_Root(t *testing.T) {
	ctx := context.Background()
	ctx = StartSpan(ctx, "trace-1", "root-span")
	span, ok := SpanFromContext(ctx)
	if !ok || span == nil {
		t.Fatalf("expected span in context")
	}
	if span.TraceID != "trace-1" {
		t.Fatalf("expected traceID trace-1, got %s", span.TraceID)
	}
	if span.Name != "root-span" {
		t.Fatalf("expected name root-span, got %s", span.Name)
	}
	if span.ParentID != "" {
		t.Fatalf("expected empty parentID for root, got %s", span.ParentID)
	}
}

func TestStartSpan_Child(t *testing.T) {
	ctx := context.Background()
	ctx = StartSpan(ctx, "trace-1", "parent")
	parent, _ := SpanFromContext(ctx)
	ctx = StartSpan(ctx, "trace-1", "child")
	child, ok := SpanFromContext(ctx)
	if !ok || child == nil {
		t.Fatalf("expected child span in context")
	}
	if child.ParentID != parent.SpanID {
		t.Fatalf("expected child ParentID %s, got %s", parent.SpanID, child.ParentID)
	}
}

func TestStartSpan_TraceIDPropagation(t *testing.T) {
	ctx := context.Background()
	ctx = StartSpan(ctx, "trace-abc", "first")
	ctx = StartSpan(ctx, "trace-abc", "second")
	span, _ := SpanFromContext(ctx)
	if span.TraceID != "trace-abc" {
		t.Fatalf("expected traceID trace-abc, got %s", span.TraceID)
	}
}

func TestFinishSpan(t *testing.T) {
	ctx := context.Background()
	ctx = StartSpan(ctx, "trace-1", "finish-test")
	span, _ := SpanFromContext(ctx)
	span.Started = time.Now().Add(-10 * time.Millisecond)
	FinishSpan(ctx, map[string]string{"key": "value"})
}

func TestFinishSpan_NoSpan(t *testing.T) {
	ctx := context.Background()
	FinishSpan(ctx)
}

func TestSpanFromContext_Exists(t *testing.T) {
	ctx := context.Background()
	ctx = StartSpan(ctx, "trace-1", "exists")
	span, ok := SpanFromContext(ctx)
	if !ok || span == nil {
		t.Fatalf("expected span to exist")
	}
}

func TestSpanFromContext_Missing(t *testing.T) {
	ctx := context.Background()
	span, ok := SpanFromContext(ctx)
	if ok || span != nil {
		t.Fatalf("expected no span, got %+v", span)
	}
}
