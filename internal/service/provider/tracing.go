package provider

import "context"

type traceCtxKey struct{}

// TraceContext holds request-scoped tracing identifiers.
type TraceContext struct {
	RequestID   string
	Traceparent string
}

// ContextWithTraceContext injects trace info into ctx for upstream propagation.
func ContextWithTraceContext(ctx context.Context, tc *TraceContext) context.Context {
	if tc == nil {
		return ctx
	}
	return context.WithValue(ctx, traceCtxKey{}, tc)
}

func traceContextFromContext(ctx context.Context) (*TraceContext, bool) {
	tc, ok := ctx.Value(traceCtxKey{}).(*TraceContext)
	return tc, ok
}
