package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gin-gonic/gin"
)

func TestSetIdentity_RoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

	want := &repository.AuthIdentity{APIKeyID: "key-1", Role: repository.RoleTenantUser}
	SetIdentity(c, want)

	got, ok := Identity(c)
	if !ok {
		t.Fatal("Identity not found")
	}
	if got.APIKeyID != want.APIKeyID || got.Role != want.Role {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestIdentity_Missing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	_, ok := Identity(c)
	if ok {
		t.Fatal("expected missing identity")
	}
}

func TestSetRequestMeta_RoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	want := &RequestMeta{Model: "gpt-4", EstimatedTokens: 42}
	SetRequestMeta(c, want)

	got, ok := GetRequestMeta(c)
	if !ok {
		t.Fatal("GetRequestMeta not found")
	}
	if got.Model != want.Model || got.EstimatedTokens != want.EstimatedTokens {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSetRequestContext_RoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/test", nil)

	want := &RequestContext{RequestID: "req-1", TraceID: "trace-1", Traceparent: "tp-1"}
	SetRequestContext(c, want)

	got, ok := GetRequestContext(c)
	if !ok {
		t.Fatal("GetRequestContext not found")
	}
	if got.RequestID != want.RequestID {
		t.Fatalf("got %q, want %q", got.RequestID, want.RequestID)
	}

	fromStd, ok := RequestContextFromContext(c.Request.Context())
	if !ok {
		t.Fatal("RequestContextFromContext not found")
	}
	if fromStd.TraceID != want.TraceID {
		t.Fatalf("got %q, want %q", fromStd.TraceID, want.TraceID)
	}
}

func TestRequestContextFromContext_Missing(t *testing.T) {
	ctx := context.Background()
	_, ok := RequestContextFromContext(ctx)
	if ok {
		t.Fatal("expected missing request context")
	}
}
