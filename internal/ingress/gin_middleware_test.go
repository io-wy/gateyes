package ingress

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestMiddleware_Handler_RateLimit(t *testing.T) {
	block := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // hold the connection until test signals release
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	b := proxy.NewBackend("b1", upstream.Listener.Addr().String(), "http", 1)
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{
			ID:          "rl-test",
			Host:        "api.example.com",
			Path:        "/",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{b}),
			Annotations: &proxy.Annotations{
				RateLimitConnections: 1,
			},
		},
	})
	mw := NewMiddleware(MiddlewareOpts{
		RouteTable: rt,
		Proxy:      proxy.NewProxy(proxy.DefaultProxyConfig()),
		Enabled:    true,
	})

	r := setupGin()
	r.Use(mw.Handler())

	// First request runs concurrently and blocks in the upstream handler.
	req1 := httptest.NewRequest("GET", "http://api.example.com/health", nil)
	rec1 := httptest.NewRecorder()
	go r.ServeHTTP(rec1, req1)

	// Wait briefly to ensure the first request has acquired the connection slot.
	time.Sleep(50 * time.Millisecond)

	// Second request from same IP should be rejected while first is still active.
	req2 := httptest.NewRequest("GET", "http://api.example.com/health", nil)
	req2.RemoteAddr = req1.RemoteAddr
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request status = %d, want 429", rec2.Code)
	}

	close(block)
	// Give the first request time to finish after unblocking.
	time.Sleep(50 * time.Millisecond)
}

func TestMiddleware_Handler_CookieAffinity(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	b1 := proxy.NewBackend("b1", upstream.Listener.Addr().String(), "http", 1)
	b2 := proxy.NewBackend("b2", upstream.Listener.Addr().String(), "http", 1)
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{
			ID:          "aff-test",
			Host:        "api.example.com",
			Path:        "/",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{b1, b2}),
			Annotations: &proxy.Annotations{
				Affinity:           "cookie",
				AffinityCookieName: "session",
			},
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

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Response should set affinity cookie.
	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie to be set")
	}
	if sessionCookie.Value == "" {
		t.Error("expected non-empty session cookie value")
	}
}

func TestMiddleware_Handler_IPWhitelistDeny(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	b := proxy.NewBackend("b1", upstream.Listener.Addr().String(), "http", 1)
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{
			ID:          "wl-test",
			Host:        "api.example.com",
			Path:        "/",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{b}),
			Annotations: &proxy.Annotations{
				WhitelistSourceRange: []string{"10.0.0.0/8"},
			},
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
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestMiddleware_Handler_BodySizeLimit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	b := proxy.NewBackend("b1", upstream.Listener.Addr().String(), "http", 1)
	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{
			ID:          "bs-test",
			Host:        "api.example.com",
			Path:        "/",
			PathType:    proxy.PathTypePrefix,
			BackendPool: proxy.NewBackendPool([]proxy.Backend{b}),
			Annotations: &proxy.Annotations{
				ProxyBodySize: 10,
			},
		},
	})
	mw := NewMiddleware(MiddlewareOpts{
		RouteTable: rt,
		Proxy:      proxy.NewProxy(proxy.DefaultProxyConfig()),
		Enabled:    true,
	})

	r := setupGin()
	r.Use(mw.Handler())

	// Request body exceeds 10 bytes.
	req := httptest.NewRequest("POST", "http://api.example.com/upload", strings.NewReader("this body is too large"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// MaxBytesReader returns 413 when body exceeds limit.
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}

func TestMiddleware_Handler_Canary(t *testing.T) {
	stableUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "stable")
		w.WriteHeader(http.StatusOK)
	}))
	defer stableUpstream.Close()

	canaryUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "canary")
		w.WriteHeader(http.StatusOK)
	}))
	defer canaryUpstream.Close()

	stable := proxy.NewBackend("stable", stableUpstream.Listener.Addr().String(), "http", 1)
	canary := proxy.NewBackend("canary", canaryUpstream.Listener.Addr().String(), "http", 1)

	rt := NewRouteTable()
	rt.Replace([]proxy.RouteRule{
		{
			ID:                "canary-test",
			Host:              "api.example.com",
			Path:              "/",
			PathType:          proxy.PathTypePrefix,
			BackendPool:       proxy.NewBackendPool([]proxy.Backend{stable}),
			CanaryBackendPool: proxy.NewBackendPool([]proxy.Backend{canary}),
			Annotations: &proxy.Annotations{
				Canary:       true,
				CanaryWeight: 100, // always canary for deterministic test
			},
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

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-Backend"); got != "canary" {
		t.Errorf("X-Backend = %q, want canary", got)
	}
}
