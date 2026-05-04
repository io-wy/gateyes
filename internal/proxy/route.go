package proxy

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// PathType mirrors networking.k8s.io/v1 PathType.
type PathType string

const (
	PathTypeExact   PathType = "Exact"
	PathTypePrefix  PathType = "Prefix"
	PathTypeRegular PathType = "ImplementationSpecific"
)

// Annotations holds nginx ingress annotations translated to internal config.
type Annotations struct {
	RewriteTarget        string            // nginx.ingress.kubernetes.io/rewrite-target
	SSLRedirect          bool              // nginx.ingress.kubernetes.io/ssl-redirect
	ProxyBodySize        int64             // nginx.ingress.kubernetes.io/proxy-body-size (bytes)
	ProxyReadTimeout     time.Duration     // nginx.ingress.kubernetes.io/proxy-read-timeout
	ProxySendTimeout     time.Duration     // nginx.ingress.kubernetes.io/proxy-send-timeout
	ProxyConnectTimeout  time.Duration     // nginx.ingress.kubernetes.io/proxy-connect-timeout
	EnableCORS           bool              // nginx.ingress.kubernetes.io/enable-cors
	CORSAllowOrigin      []string          // nginx.ingress.kubernetes.io/cors-allow-origin
	CORSAllowMethods     []string          // nginx.ingress.kubernetes.io/cors-allow-methods
	CORSAllowHeaders     []string          // nginx.ingress.kubernetes.io/cors-allow-headers
	CORSAllowCredentials bool              // nginx.ingress.kubernetes.io/cors-allow-credentials
	RateLimitRPS         float64           // nginx.ingress.kubernetes.io/limit-rps
	RateLimitConnections int               // nginx.ingress.kubernetes.io/limit-connections
	Affinity             string            // nginx.ingress.kubernetes.io/affinity (cookie)
	AffinityCookieName   string            // nginx.ingress.kubernetes.io/session-cookie-name
	Canary               bool              // nginx.ingress.kubernetes.io/canary
	CanaryWeight         int               // nginx.ingress.kubernetes.io/canary-weight (0-100)
	CanaryByHeader       string            // nginx.ingress.kubernetes.io/canary-by-header
	WhitelistSourceRange []string          // nginx.ingress.kubernetes.io/whitelist-source-range
	BackendProtocol      string            // nginx.ingress.kubernetes.io/backend-protocol (HTTP, HTTPS, GRPC)
	ProxyNextUpstream    bool              // nginx.ingress.kubernetes.io/proxy-next-upstream
	ProxyNextUpstreamTries int             // nginx.ingress.kubernetes.io/proxy-next-upstream-tries
	Raw                  map[string]string // untouched annotations
}

// RouteRule is a single ingress routing rule.
type RouteRule struct {
	ID          string
	Host        string            // empty = wildcard
	Path        string
	PathType    PathType
	BackendPool *BackendPool
	Annotations *Annotations
}

// Match checks whether an HTTP request matches this route.
func (r *RouteRule) Match(req *http.Request) bool {
	if r.Host != "" && !hostMatches(r.Host, req.Host) {
		return false
	}
	switch r.PathType {
	case PathTypeExact:
		return req.URL.Path == r.Path
	case PathTypeRegular:
		// ImplementationSpecific: treat as prefix for now; advanced regex via annotation.
		return strings.HasPrefix(req.URL.Path, r.Path)
	default: // PathTypePrefix or empty
		return strings.HasPrefix(req.URL.Path, r.Path)
	}
}

func hostMatches(pattern, host string) {
	// Strip port if present.
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	if pattern == host {
		return true
	}
	// Wildcard subdomain: *.example.com
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // .example.com
		return strings.HasSuffix(host, suffix)
	}
	return false
}

// RewritePath applies rewrite-target annotation to the request path.
func (r *RouteRule) RewritePath(original string) string {
	if r.Annotations == nil || r.Annotations.RewriteTarget == "" {
		return original
	}
	target := r.Annotations.RewriteTarget
	// Simple prefix rewrite: if target contains regex capture refs, treat as literal replacement.
	if strings.Contains(target, "$") {
		// Advanced regex rewrite not supported in MVP.
		return original
	}
	if target == "/" {
		return original[len(r.Path):]
	}
	return target + original[len(r.Path):]
}

// UpstreamURL builds the full upstream URL for a backend.
func (r *RouteRule) UpstreamURL(b Backend, originalPath string) string {
	scheme := b.Protocol()
	if scheme == "" {
		scheme = "http"
	}
	path := r.RewritePath(originalPath)
	return fmt.Sprintf("%s://%s%s", scheme, b.Address(), path)
}
