package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewProxy_DefaultConfig(t *testing.T) {
	p := NewProxy(DefaultProxyConfig())
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
}

func TestProxy_ServeHTTP(t *testing.T) {
	// Create a fake upstream.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello from upstream"))
	}))
	defer upstream.Close()

	p := NewProxy(DefaultProxyConfig())
	b := NewBackend("b1", upstream.Listener.Addr().String(), "http", 1)
	rule := RouteRule{
		Path:        "/api/",
		PathType:    PathTypePrefix,
		BackendPool: NewBackendPool([]Backend{b}),
	}

	req := httptest.NewRequest("GET", "http://example.com/api/health", nil)
	rec := httptest.NewRecorder()

	if err := p.ServeHTTP(rec, req, &rule, b); err != nil {
		t.Fatalf("ServeHTTP error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "hello from upstream" {
		t.Errorf("body = %q, want hello from upstream", body)
	}
}

func TestProxy_ServeHTTP_CORS(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	p := NewProxy(DefaultProxyConfig())
	b := NewBackend("b1", upstream.Listener.Addr().String(), "http", 1)
	rule := RouteRule{
		Path:     "/",
		PathType: PathTypePrefix,
		Annotations: &Annotations{
			EnableCORS:       true,
			CORSAllowOrigin:  []string{"https://app.example.com"},
			CORSAllowMethods: []string{"GET", "POST"},
		},
		BackendPool: NewBackendPool([]Backend{b}),
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()

	if err := p.ServeHTTP(rec, req, &rule, b); err != nil {
		t.Fatalf("ServeHTTP error: %v", err)
	}

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("CORS origin = %q, want https://app.example.com", got)
	}
}

func TestProxy_ServeHTTP_NoBackend(t *testing.T) {
	p := NewProxy(DefaultProxyConfig())
	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	if err := p.ServeHTTP(rec, req, &RouteRule{}, nil); err == nil {
		t.Error("expected error for nil backend")
	}
}

func TestProxy_HandlePreflight(t *testing.T) {
	p := NewProxy(DefaultProxyConfig())
	annot := &Annotations{
		EnableCORS:           true,
		CORSAllowMethods:     []string{"GET", "POST", "OPTIONS"},
		CORSAllowHeaders:     []string{"Content-Type", "Authorization"},
		CORSAllowCredentials: true,
	}
	req := httptest.NewRequest("OPTIONS", "http://example.com/", nil)
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()

	p.HandlePreflight(rec, annot, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("expected CORS methods header")
	}
}

func TestProxy_HandlePreflight_Disabled(t *testing.T) {
	p := NewProxy(DefaultProxyConfig())
	req := httptest.NewRequest("OPTIONS", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	p.HandlePreflight(rec, &Annotations{EnableCORS: false}, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestProxyAnnotatedTransport(t *testing.T) {
	p := NewProxy(DefaultProxyConfig())
	annot := &Annotations{
		ProxyConnectTimeout: 3 * time.Second,
	}
	rt := p.annotatedTransport(annot)
	if rt == nil {
		t.Error("expected non-nil transport")
	}
}

func TestIsWebSocketUpgrade(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Upgrade", "websocket")
	if !IsWebSocketUpgrade(req) {
		t.Error("expected WebSocket upgrade detected")
	}
	req2 := httptest.NewRequest("GET", "/", nil)
	if IsWebSocketUpgrade(req2) {
		t.Error("expected no WebSocket upgrade for plain request")
	}
}

// --- Retry logic tests ---

func TestProxy_ServeHTTPWithRetry_FirstFailsSecondSucceeds(t *testing.T) {
	badUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer badUpstream.Close()

	goodUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))
	defer goodUpstream.Close()

	p := NewProxy(DefaultProxyConfig())
	badBackend := NewBackend("bad", badUpstream.Listener.Addr().String(), "http", 1)
	goodBackend := NewBackend("good", goodUpstream.Listener.Addr().String(), "http", 1)

	rule := RouteRule{
		Path:     "/",
		PathType: PathTypePrefix,
		Annotations: &Annotations{
			ProxyNextUpstream:      true,
			ProxyNextUpstreamTries: 2,
		},
		BackendPool: NewBackendPool([]Backend{badBackend, goodBackend}),
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	if err := p.ServeHTTPWithRetry(rec, req, &rule, []Backend{badBackend, goodBackend}); err != nil {
		t.Fatalf("ServeHTTPWithRetry error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != "success" {
		t.Errorf("body = %q, want success", body)
	}
}

func TestProxy_ServeHTTPWithRetry_AllBackendsFail(t *testing.T) {
	badUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer badUpstream.Close()

	p := NewProxy(DefaultProxyConfig())
	b1 := NewBackend("b1", badUpstream.Listener.Addr().String(), "http", 1)
	b2 := NewBackend("b2", badUpstream.Listener.Addr().String(), "http", 1)

	rule := RouteRule{
		Path:     "/",
		PathType: PathTypePrefix,
		Annotations: &Annotations{
			ProxyNextUpstream:      true,
			ProxyNextUpstreamTries: 2,
		},
		BackendPool: NewBackendPool([]Backend{b1, b2}),
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	// When all backends fail with 5xx, the last attempt serves directly.
	// serveOnce uses ReverseProxy which returns nil error even for 5xx responses.
	err := p.ServeHTTPWithRetry(rec, req, &rule, []Backend{b1, b2})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// The last backend returns 503, written directly to response.
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestProxy_ServeHTTPWithRetry_Disabled(t *testing.T) {
	badUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer badUpstream.Close()

	goodUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer goodUpstream.Close()

	p := NewProxy(DefaultProxyConfig())
	badBackend := NewBackend("bad", badUpstream.Listener.Addr().String(), "http", 1)
	goodBackend := NewBackend("good", goodUpstream.Listener.Addr().String(), "http", 1)

	// ProxyNextUpstream is false (disabled).
	rule := RouteRule{
		Path:     "/",
		PathType: PathTypePrefix,
		Annotations: &Annotations{
			ProxyNextUpstream:      false,
			ProxyNextUpstreamTries: 2,
		},
		BackendPool: NewBackendPool([]Backend{badBackend, goodBackend}),
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	// When retry is disabled, only the first backend should be tried.
	// The response should be the 503 from the first backend.
	if err := p.ServeHTTPWithRetry(rec, req, &rule, []Backend{badBackend, goodBackend}); err != nil {
		t.Fatalf("ServeHTTPWithRetry error: %v", err)
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestProxy_ServeHTTPWithRetry_NoBackends(t *testing.T) {
	p := NewProxy(DefaultProxyConfig())
	rule := RouteRule{Path: "/", PathType: PathTypePrefix}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	if err := p.ServeHTTPWithRetry(rec, req, &rule, nil); err == nil {
		t.Error("expected error for nil backends")
	}
}

func TestProxy_ServeHTTPWithRetry_DefaultTries(t *testing.T) {
	badUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer badUpstream.Close()

	goodUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer goodUpstream.Close()

	p := NewProxy(DefaultProxyConfig())
	badBackend := NewBackend("bad", badUpstream.Listener.Addr().String(), "http", 1)
	goodBackend := NewBackend("good", goodUpstream.Listener.Addr().String(), "http", 1)

	// ProxyNextUpstream enabled but tries not set (defaults to all backends).
	rule := RouteRule{
		Path:     "/",
		PathType: PathTypePrefix,
		Annotations: &Annotations{
			ProxyNextUpstream:      true,
			ProxyNextUpstreamTries: 0, // 0 means use all backends
		},
		BackendPool: NewBackendPool([]Backend{badBackend, goodBackend}),
	}

	req := httptest.NewRequest("GET", "http://example.com/", nil)
	rec := httptest.NewRecorder()

	if err := p.ServeHTTPWithRetry(rec, req, &rule, []Backend{badBackend, goodBackend}); err != nil {
		t.Fatalf("ServeHTTPWithRetry error: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
