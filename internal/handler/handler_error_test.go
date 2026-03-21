package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

func TestInferHTTPStatusAndRenderServiceError(t *testing.T) {
	h := &Handler{metrics: NewMetrics("handler_error_test")}

	tests := []struct {
		err  error
		want int
	}{
		{err: responseSvc.ErrNoProvider, want: http.StatusServiceUnavailable},
		{err: errors.New("timeout while calling upstream"), want: http.StatusGatewayTimeout},
		{err: errors.New("401 authentication failed"), want: http.StatusUnauthorized},
		{err: errors.New("403 forbidden"), want: http.StatusForbidden},
		{err: errors.New("429 rate_limit exceeded"), want: http.StatusTooManyRequests},
		{err: errors.New("400 invalid request"), want: http.StatusBadRequest},
		{err: errors.New("boom"), want: http.StatusBadGateway},
	}
	for _, tt := range tests {
		if got := h.inferHTTPStatus(tt.err); got != tt.want {
			t.Fatalf("inferHTTPStatus(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	h.renderServiceError(c, "gpt-test", errors.New("429 rate_limit exceeded"))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("renderServiceError() status = %d, want %d: %s", rec.Code, http.StatusTooManyRequests, rec.Body.String())
	}
}
