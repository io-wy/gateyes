package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCorrelation_SetsRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Correlation())
	engine.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	rid := rec.Header().Get(RequestIDHeader)
	if rid == "" {
		t.Fatal("expected X-Request-ID header")
	}
	if len(rid) != 24 {
		t.Fatalf("expected 24-char hex, got %d chars", len(rid))
	}
}

func TestCorrelation_PropagatesTraceparent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Correlation())
	engine.GET("/test", func(c *gin.Context) {
		ctx, _ := GetRequestContext(c)
		c.JSON(http.StatusOK, gin.H{"trace_id": ctx.TraceID, "traceparent": ctx.Traceparent})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(TraceparentHeader, "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(TraceparentHeader); got != "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01" {
		t.Fatalf("traceparent not propagated: %q", got)
	}
}

func TestParseTraceID_Valid(t *testing.T) {
	traceID := parseTraceID("00-0123456789abcdef0123456789abcdef-0123456789abcdef-01")
	if traceID != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("got %q, want trace ID", traceID)
	}
}

func TestParseTraceID_Invalid(t *testing.T) {
	cases := []string{
		"",
		"00-abc-01",
		"invalid",
		"00-0123456789abcdef0123456789abcdef-0123456789abcdef-01-extra",
	}
	for _, tc := range cases {
		if got := parseTraceID(tc); got != "" {
			t.Fatalf("parseTraceID(%q) = %q, want empty", tc, got)
		}
	}
}

func TestOtelTrace_StartsSpan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(OtelTrace())
	engine.GET("/test", func(c *gin.Context) {
		span := SpanFromContext(c.Request.Context())
		if span == nil {
			t.Fatal("expected span in context")
		}
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestSpanFromContext_Missing(t *testing.T) {
	ctx := httptest.NewRequest(http.MethodGet, "/", nil).Context()
	span := SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		t.Fatal("expected invalid span for context without trace")
	}
}
