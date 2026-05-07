package ingress

import (
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/proxy"
)

func TestParseAnnotations_RewriteAndTimeouts(t *testing.T) {
	raw := map[string]string{
		"nginx.ingress.kubernetes.io/rewrite-target":         "/v2",
		"nginx.ingress.kubernetes.io/proxy-read-timeout":     "120",
		"nginx.ingress.kubernetes.io/proxy-connect-timeout":  "10",
		"nginx.ingress.kubernetes.io/proxy-body-size":        "10m",
	}
	a := ParseAnnotations(raw)
	if a.RewriteTarget != "/v2" {
		t.Errorf("RewriteTarget = %q, want /v2", a.RewriteTarget)
	}
	if a.ProxyReadTimeout != 120*time.Second {
		t.Errorf("ProxyReadTimeout = %v, want 120s", a.ProxyReadTimeout)
	}
	if a.ProxyConnectTimeout != 10*time.Second {
		t.Errorf("ProxyConnectTimeout = %v, want 10s", a.ProxyConnectTimeout)
	}
	if a.ProxyBodySize != 10*1000*1000 {
		t.Errorf("ProxyBodySize = %d, want 10M", a.ProxyBodySize)
	}
}

func TestParseAnnotations_CORS(t *testing.T) {
	raw := map[string]string{
		"nginx.ingress.kubernetes.io/enable-cors":              "true",
		"nginx.ingress.kubernetes.io/cors-allow-origin":        "https://app.example.com, https://admin.example.com",
		"nginx.ingress.kubernetes.io/cors-allow-methods":       "GET, POST",
		"nginx.ingress.kubernetes.io/cors-allow-headers":       "X-Custom",
		"nginx.ingress.kubernetes.io/cors-allow-credentials":   "true",
	}
	a := ParseAnnotations(raw)
	if !a.EnableCORS {
		t.Error("expected EnableCORS = true")
	}
	if len(a.CORSAllowOrigin) != 2 {
		t.Errorf("CORSAllowOrigin len = %d, want 2", len(a.CORSAllowOrigin))
	}
	if !a.CORSAllowCredentials {
		t.Error("expected CORSAllowCredentials = true")
	}
}

func TestParseAnnotations_SSLRedirect(t *testing.T) {
	a := ParseAnnotations(map[string]string{
		"nginx.ingress.kubernetes.io/ssl-redirect": "true",
	})
	if !a.SSLRedirect {
		t.Error("expected SSLRedirect = true")
	}
}

func TestParseAnnotations_Canary(t *testing.T) {
	raw := map[string]string{
		"nginx.ingress.kubernetes.io/canary":        "true",
		"nginx.ingress.kubernetes.io/canary-weight": "20",
		"nginx.ingress.kubernetes.io/canary-by-header": "X-Canary",
	}
	a := ParseAnnotations(raw)
	if !a.Canary {
		t.Error("expected Canary = true")
	}
	if a.CanaryWeight != 20 {
		t.Errorf("CanaryWeight = %d, want 20", a.CanaryWeight)
	}
	if a.CanaryByHeader != "X-Canary" {
		t.Errorf("CanaryByHeader = %q, want X-Canary", a.CanaryByHeader)
	}
}

func TestParseAnnotations_RateLimit(t *testing.T) {
	raw := map[string]string{
		"nginx.ingress.kubernetes.io/limit-rps":        "100",
		"nginx.ingress.kubernetes.io/limit-connections": "50",
	}
	a := ParseAnnotations(raw)
	if a.RateLimitRPS != 100 {
		t.Errorf("RateLimitRPS = %f, want 100", a.RateLimitRPS)
	}
	if a.RateLimitConnections != 50 {
		t.Errorf("RateLimitConnections = %d, want 50", a.RateLimitConnections)
	}
}

func TestParseAnnotations_BackendProtocol(t *testing.T) {
	a := ParseAnnotations(map[string]string{
		"nginx.ingress.kubernetes.io/backend-protocol": "HTTPS",
	})
	if a.BackendProtocol != "HTTPS" {
		t.Errorf("BackendProtocol = %q, want HTTPS", a.BackendProtocol)
	}
}

func TestParseAnnotations_Whitelist(t *testing.T) {
	a := ParseAnnotations(map[string]string{
		"nginx.ingress.kubernetes.io/whitelist-source-range": "10.0.0.0/8, 192.168.0.0/16",
	})
	if len(a.WhitelistSourceRange) != 2 {
		t.Errorf("WhitelistSourceRange len = %d, want 2", len(a.WhitelistSourceRange))
	}
}

func TestParseAnnotations_Affinity(t *testing.T) {
	raw := map[string]string{
		"nginx.ingress.kubernetes.io/affinity":              "cookie",
		"nginx.ingress.kubernetes.io/session-cookie-name":   "INGRESS_SESSION",
	}
	a := ParseAnnotations(raw)
	if a.Affinity != "cookie" {
		t.Errorf("Affinity = %q, want cookie", a.Affinity)
	}
	if a.AffinityCookieName != "INGRESS_SESSION" {
		t.Errorf("AffinityCookieName = %q, want INGRESS_SESSION", a.AffinityCookieName)
	}
}

func TestParseAnnotations_RawPreserved(t *testing.T) {
	raw := map[string]string{"custom.key": "value"}
	a := ParseAnnotations(raw)
	if a.Raw["custom.key"] != "value" {
		t.Error("expected raw annotation preserved")
	}
}

func TestParseAnnotations_ProxyNextUpstream(t *testing.T) {
	raw := map[string]string{
		"nginx.ingress.kubernetes.io/proxy-next-upstream":       "true",
		"nginx.ingress.kubernetes.io/proxy-next-upstream-tries": "3",
	}
	a := ParseAnnotations(raw)
	if !a.ProxyNextUpstream {
		t.Error("expected ProxyNextUpstream = true")
	}
	if a.ProxyNextUpstreamTries != 3 {
		t.Errorf("ProxyNextUpstreamTries = %d, want 3", a.ProxyNextUpstreamTries)
	}
}

func TestParseAnnotations_ProxySendTimeout(t *testing.T) {
	a := ParseAnnotations(map[string]string{
		"nginx.ingress.kubernetes.io/proxy-send-timeout": "30",
	})
	if a.ProxySendTimeout != 30*time.Second {
		t.Errorf("ProxySendTimeout = %v, want 30s", a.ProxySendTimeout)
	}
}

func TestParseAnnotations_Empty(t *testing.T) {
	a := ParseAnnotations(map[string]string{})
	if a == nil {
		t.Fatal("expected non-nil Annotations for empty input")
	}
	if a.RewriteTarget != "" {
		t.Errorf("expected empty RewriteTarget, got %q", a.RewriteTarget)
	}
	if a.Canary {
		t.Error("expected Canary = false")
	}
	if a.RateLimitRPS != 0 {
		t.Errorf("expected RateLimitRPS = 0, got %f", a.RateLimitRPS)
	}
}

func TestParseAnnotations_ProxyBodySize_Binary(t *testing.T) {
	a := ParseAnnotations(map[string]string{
		"nginx.ingress.kubernetes.io/proxy-body-size": "1Gi",
	})
	want := int64(1024 * 1024 * 1024)
	if a.ProxyBodySize != want {
		t.Errorf("ProxyBodySize = %d, want %d", a.ProxyBodySize, want)
	}
}

func TestRouteScore(t *testing.T) {
	r1 := proxy.RouteRule{Host: "api.example.com", Path: "/v1", PathType: proxy.PathTypeExact}
	r2 := proxy.RouteRule{Host: "*.example.com", Path: "/", PathType: proxy.PathTypePrefix}
	if routeScore(&r1, nil) <= routeScore(&r2, nil) {
		t.Error("exact host + exact path should score higher than wildcard + prefix")
	}
}
