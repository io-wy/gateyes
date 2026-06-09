package middleware

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"

	gatewaytrace "github.com/gateyes/gateway/pkg/trace"
)

func OtelTrace() gin.HandlerFunc {
	prop := otel.GetTextMapPropagator()
	return func(c *gin.Context) {
		ctx := prop.Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		spanName := c.Request.Method + " " + c.FullPath()
		if spanName == "" {
			spanName = c.Request.Method + " " + c.Request.URL.Path
		}

		ctx, span := gatewaytrace.StartOTelSpan(ctx, spanName,
			attribute.String("http.method", c.Request.Method),
			attribute.String("http.path", c.Request.URL.Path),
			attribute.String("http.route", c.FullPath()),
			attribute.String("http.target", c.Request.URL.Path),
			attribute.String("http.host", c.Request.Host),
			attribute.String("http.scheme", c.Request.URL.Scheme),
			attribute.String("http.user_agent", c.Request.UserAgent()),
		)
		defer span.End()

		c.Request = c.Request.WithContext(ctx)
		c.Next()

		span.SetAttributes(
			attribute.Int("http.status_code", c.Writer.Status()),
		)
		if c.Writer.Status() >= 500 {
			span.SetStatus(codes.Error, "server error")
		}
	}
}
