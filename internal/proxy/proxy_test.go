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
