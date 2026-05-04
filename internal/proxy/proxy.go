package proxy

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// Proxy is a dynamic reverse proxy for ingress traffic.
type Proxy struct {
	transport   http.RoundTripper
	logger      *slog.Logger
	dialTimeout time.Duration
}

// ProxyConfig holds proxy-level settings.
type ProxyConfig struct {
	ConnectTimeout  time.Duration
	ReadTimeout     time.Duration
	SendTimeout     time.Duration
	IdleConnTimeout time.Duration
	MaxIdleConns    int
	MaxConnsPerHost int
}

func DefaultProxyConfig() ProxyConfig {
	return ProxyConfig{
		ConnectTimeout:  5 * time.Second,
		ReadTimeout:     60 * time.Second,
		SendTimeout:     60 * time.Second,
		IdleConnTimeout: 90 * time.Second,
		MaxIdleConns:    100,
		MaxConnsPerHost: 10,
	}
}

func NewProxy(cfg ProxyConfig) *Proxy {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   cfg.ConnectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxConnsPerHost,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &Proxy{
		transport:   transport,
		logger:      slog.With("component", "proxy"),
		dialTimeout: cfg.ConnectTimeout,
	}
}

// ServeHTTP proxies the request to the selected backend.
// For backward compatibility, delegates to ServeHTTPWithRetry with a single backend.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, req *http.Request, rule *RouteRule, backend Backend) error {
	return p.ServeHTTPWithRetry(w, req, rule, []Backend{backend})
}

// ServeHTTPWithRetry proxies the request with retry support across multiple backends.
// If ProxyNextUpstream is enabled and a backend returns a 5xx or connection error,
// it tries the next backend up to ProxyNextUpstreamTries times.
func (p *Proxy) ServeHTTPWithRetry(w http.ResponseWriter, req *http.Request, rule *RouteRule, backends []Backend) error {
	if len(backends) == 0 {
		return fmt.Errorf("no backend available")
	}

	// Determine retry configuration.
	maxTries := 1
	retryEnabled := false
	if rule.Annotations != nil && rule.Annotations.ProxyNextUpstream {
		retryEnabled = true
		maxTries = rule.Annotations.ProxyNextUpstreamTries
		if maxTries <= 0 {
			maxTries = len(backends)
		}
		if maxTries > len(backends) {
			maxTries = len(backends)
		}
	}

	var lastErr error
	var lastStatusCode int

	for i := 0; i < maxTries && i < len(backends); i++ {
		backend := backends[i]
		if backend == nil {
			lastErr = fmt.Errorf("backend at index %d is nil", i)
			continue
		}

		// Use a response recorder to capture the result without writing to w directly.
		// On the last attempt or success, write through.
		// For non-last attempts with retry enabled, check if we should retry.
		shouldRetry := retryEnabled && i < maxTries-1 && i < len(backends)-1

		if shouldRetry {
			rec := httptest.NewRecorder()
			err := p.serveOnce(rec, req, rule, backend)
			if err == nil && rec.Code < 500 {
				// Success — copy response to w and return.
				copyResponse(w, rec)
				return nil
			}
			// Failure — log and retry.
			lastErr = err
			if lastErr == nil {
				lastStatusCode = rec.Code
				lastErr = fmt.Errorf("backend %s returned status %d", backend.Name(), rec.Code)
			}
			p.logger.Warn("proxy retry",
				"backend", backend.Name(),
				"attempt", i+1,
				"error", lastErr,
			)
			continue
		}

		// Last attempt — serve directly to w.
		return p.serveOnce(w, req, rule, backend)
	}

	// All retries exhausted.
	if lastErr != nil {
		if lastStatusCode >= 500 {
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		}
		return lastErr
	}

	http.Error(w, "Bad Gateway", http.StatusBadGateway)
	return fmt.Errorf("all backends failed")
}

// serveOnce performs a single proxy attempt to one backend.
func (p *Proxy) serveOnce(w http.ResponseWriter, req *http.Request, rule *RouteRule, backend Backend) error {
	targetURL, err := url.Parse(rule.UpstreamURL(backend, req.URL.Path))
	if err != nil {
		return fmt.Errorf("parse upstream URL: %w", err)
	}

	if rule.Annotations != nil && rule.Annotations.BackendProtocol == "HTTPS" {
		targetURL.Scheme = "https"
	}

	// Convert HTTP(S) to WS(S) for WebSocket upgrades so ReverseProxy handles it correctly.
	if IsWebSocketUpgrade(req) {
		if targetURL.Scheme == "https" {
			targetURL.Scheme = "wss"
		} else {
			targetURL.Scheme = "ws"
		}
	}

	// Apply proxy timeouts from annotations if present.
	transport := p.transport
	if rule.Annotations != nil {
		transport = p.annotatedTransport(rule.Annotations)
	}

	// Use a custom ReverseProxy to avoid NewSingleHostReverseProxy double-joining paths.
	rp := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = targetURL.Scheme
			r.URL.Host = targetURL.Host
			r.URL.Path = targetURL.Path
			r.URL.RawQuery = req.URL.RawQuery
			r.Host = req.Host
		},
		Transport: transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			p.logger.Error("proxy error", "backend", backend.Name(), "error", err)
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		},
		ModifyResponse: func(resp *http.Response) error {
			// Inject CORS headers if enabled.
			if rule.Annotations != nil && rule.Annotations.EnableCORS {
				p.injectCORS(resp.Header, rule.Annotations, req)
			}
			return nil
		},
	}

	outReq := req.Clone(req.Context())
	rp.ServeHTTP(w, outReq)
	return nil
}

func (p *Proxy) annotatedTransport(annot *Annotations) http.RoundTripper {
	base := p.transport.(*http.Transport)
	// Clone and override timeouts.
	clone := base.Clone()
	if annot.ProxyConnectTimeout > 0 {
		clone.DialContext = (&net.Dialer{
			Timeout:   annot.ProxyConnectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext
	}
	return clone
}

func (p *Proxy) injectCORS(hdr http.Header, annot *Annotations, req *http.Request) {
	origin := req.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	}
	if len(annot.CORSAllowOrigin) > 0 {
		allowed := false
		for _, o := range annot.CORSAllowOrigin {
			if o == origin || o == "*" {
				allowed = true
				break
			}
		}
		if !allowed {
			return
		}
	}
	hdr.Set("Access-Control-Allow-Origin", origin)
	if annot.CORSAllowCredentials {
		hdr.Set("Access-Control-Allow-Credentials", "true")
	}
	if len(annot.CORSAllowMethods) > 0 {
		hdr.Set("Access-Control-Allow-Methods", strings.Join(annot.CORSAllowMethods, ", "))
	} else {
		hdr.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
	}
	if len(annot.CORSAllowHeaders) > 0 {
		hdr.Set("Access-Control-Allow-Headers", strings.Join(annot.CORSAllowHeaders, ", "))
	} else {
		hdr.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
	}
}

// copyResponse copies headers, status code, and body from a recorder to a ResponseWriter.
func copyResponse(w http.ResponseWriter, rec *httptest.ResponseRecorder) {
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	w.Write(rec.Body.Bytes())
}

// HandlePreflight responds to OPTIONS requests for CORS preflight.
func (p *Proxy) HandlePreflight(w http.ResponseWriter, annot *Annotations, req *http.Request) {
	if annot == nil || !annot.EnableCORS {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	origin := req.Header.Get("Origin")
	if origin == "" {
		origin = "*"
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", strings.Join(annot.CORSAllowMethods, ", "))
	w.Header().Set("Access-Control-Allow-Headers", strings.Join(annot.CORSAllowHeaders, ", "))
	if annot.CORSAllowCredentials {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.WriteHeader(http.StatusNoContent)
}

// ProxyHTTPError writes a standard proxy error response.
func ProxyHTTPError(w http.ResponseWriter, code int, msg string) {
	http.Error(w, msg, code)
}

// IsWebSocketUpgrade checks if the request is a WebSocket upgrade.
func IsWebSocketUpgrade(req *http.Request) bool {
	return strings.EqualFold(req.Header.Get("Upgrade"), "websocket")
}
