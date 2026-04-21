package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gin-gonic/gin"
)

func TestMiddlewareAuthServiceAndRequireRoles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mw := newTestMiddleware(t, repository.RoleTenantAdmin, -1, nil, nil)
	if mw.AuthService() == nil {
		t.Fatal("AuthService() = nil, want non-nil")
	}

	engine := gin.New()
	engine.GET("/admin", mw.RequireRoles(repository.RoleTenantAdmin), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("RequireRoles(missing identity) status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestExtractRequestMetaRejectsInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/guarded", strings.NewReader(`{invalid`))

	if _, err := extractRequestMeta(c); err == nil {
		t.Fatal("extractRequestMeta(invalid JSON) error = nil, want non-nil")
	}
}
