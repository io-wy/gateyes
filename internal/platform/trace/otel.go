package trace

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

var otelTracer = otel.Tracer("github.com/gateyes/gateway")

func StartOTelSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, oteltrace.Span) {
	return otelTracer.Start(ctx, name, oteltrace.WithAttributes(attrs...))
}

func OTelSpanFromContext(ctx context.Context) oteltrace.Span {
	return oteltrace.SpanFromContext(ctx)
}
