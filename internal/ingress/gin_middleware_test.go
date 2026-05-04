package ingress

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gateyes/gateway/internal/proxy"
)

func setupGin() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	return r
}

func TestMiddleware_Handler_Disabled(t *testing.T) {
	mw := NewMiddleware(MiddlewareOpts{Enabled: false})
	r := setupGin()
	r.Use(mw.Handler())
	r.GET("/ai", func(c *gin.Context) {
		c.String(http.StatusOK, "ai")
	})

	req := httptest.NewRequest("GET", "/ai", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ai" {
		t.Errorf("body = %q, want ai", rec.Body.String())
	}
}

func TestMiddleware_Handler_NoMatch(t *testing.T) {
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{Host: "api.example.com", Path: "/api", PathType: proxy.PathTypePrefix, BackendPool: proxy.NewBackendPool(nil)},
	})
	mw := NewMiddleware(MiddlewareOpts{
		RouteTable: rt,
		Proxy:      proxy.NewProxy(proxy.DefaultProxyConfig()),
		Enabled:    true,
	})

	r := setupGin()
	r.Use(mw.Handler())
	r.GET("/v1/models", func(c *gin.Context) {
		c.String(http.StatusOK, "models")
	})

	req := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "models" {
		t.Errorf("body = %q, want models", rec.Body.String())
	}
}

func TestMiddleware_Handler_SSLRedirect(t *testing.T) {
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{
			Host:        "secure.example.com",
			Path:        "/",
			PathType:    proxy.PathTypePrefix,
			Annotations: &proxy.Annotations{SSLRedirect: true},
			BackendPool: proxy.NewBackendPool(nil),
		},
	})
	mw := NewMiddleware(MiddlewareOpts{
		RouteTable: rt,
		Proxy:      proxy.NewProxy(proxy.DefaultProxyConfig()),
		Enabled:    true,
	})

	r := setupGin()
	r.Use(mw.Handler())

	req := httptest.NewRequest("GET", "http://secure.example.com/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want 301", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc != "https://secure.example.com/health" {
		t.Errorf("Location = %q, want https://secure.example.com/health", loc)
	}
}

func TestMiddleware_Handler_CORS_Preflight(t *testing.T) {
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{
			Host:        "api.example.com",
			Path:        "/",
			PathType:    proxy.PathTypePrefix,
			Annotations: &proxy.Annotations{EnableCORS: true},
			BackendPool: proxy.NewBackendPool(nil),
		},
	})
	mw := NewMiddleware(MiddlewareOpts{
		RouteTable: rt,
		Proxy:      proxy.NewProxy(proxy.DefaultProxyConfig()),
		Enabled:    true,
	})

	r := setupGin()
	r.Use(mw.Handler())

	req := httptest.NewRequest("OPTIONS", "http://api.example.com/health", nil)
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}

func TestMiddleware_Handler_NoHealthyBackends(t *testing.T) {
	b := proxy.NewBackend("b1", "10.0.0.1:8080", "http", 1)
	b.SetHealthy(false)

	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{
			Host:        "api.example.com",
			Path:        "/",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{b}),
		},
	})
	mw := NewMiddleware(MiddlewareOpts{
		RouteTable: rt,
		Proxy:      proxy.NewProxy(proxy.DefaultProxyConfig()),
		Enabled:    true,
	})

	r := setupGin()
	r.Use(mw.Handler())

	req := httptest.NewRequest("GET", "http://api.example.com/health", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
