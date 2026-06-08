package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gin-gonic/gin"
)

func TestAuthMiddleware_ValidKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mw := newTestMiddleware(t, repository.RoleTenantUser, -1, nil, nil)

	engine := gin.New()
	engine.POST("/test", mw.Auth(), func(c *gin.Context) {
		id, ok := Identity(c)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "missing identity"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"role": id.Role})
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Authorization", "Bearer test-key:test-secret")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthMiddleware_InactiveKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mw := newTestMiddleware(t, repository.RoleTenantUser, -1, nil, nil)

	engine := gin.New()
	engine.POST("/test", mw.Auth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Header.Set("Authorization", "Bearer test-key:bad-secret")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRequireRoles_Allowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mw := newTestMiddleware(t, repository.RoleTenantAdmin, -1, nil, nil)

	engine := gin.New()
	engine.GET("/admin", func(c *gin.Context) {
		SetIdentity(c, &repository.AuthIdentity{Role: repository.RoleTenantAdmin})
		c.Next()
	}, mw.RequireRoles(repository.RoleTenantAdmin), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRequireRoles_Denied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mw := newTestMiddleware(t, repository.RoleTenantUser, -1, nil, nil)

	engine := gin.New()
	engine.GET("/admin", func(c *gin.Context) {
		SetIdentity(c, &repository.AuthIdentity{Role: repository.RoleTenantUser})
		c.Next()
	}, mw.RequireRoles(repository.RoleTenantAdmin), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}
