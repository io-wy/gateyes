package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	redispkg "github.com/gateyes/gateway/internal/pkg/redis"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func RedisClientConfig(cfg config.RedisConfig) redispkg.Config {
	return redispkg.Config{
		Addr:           cfg.Addr,
		Password:       cfg.Password,
		DB:             cfg.DB,
		MinIdleConns:   cfg.MinIdleConns,
		MaxRetries:     cfg.MaxRetries,
		PoolSize:       cfg.PoolSize,
		DialTimeoutMs:  cfg.DialTimeoutMs,
		ReadTimeoutMs:  cfg.ReadTimeoutMs,
		WriteTimeoutMs: cfg.WriteTimeoutMs,
	}
}

func InitTracer(cfg config.TracingConfig) func() {
	if !cfg.Enabled {
		return func() {}
	}

	var exporter sdktrace.SpanExporter
	var err error

	switch cfg.Exporter {
	case "otlp":
		opts := []otlptracehttp.Option{}
		if cfg.Endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpointURL(cfg.Endpoint))
		}
		exporter, err = otlptracehttp.New(context.Background(), opts...)
		if err != nil {
			slog.Warn("failed to create OTLP trace exporter", "error", err)
			return func() {}
		}
	default:
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			slog.Warn("failed to create stdout trace exporter", "error", err)
			return func() {}
		}
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("gateyes-gateway"),
			semconv.ServiceVersion("1.0.0"),
		)),
	)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTracerTimeout)
		defer cancel()
		if err := provider.Shutdown(ctx); err != nil {
			slog.Warn("failed to shutdown tracer provider", "error", err)
		}
	}
}

const shutdownTracerTimeout = 5 * time.Second
