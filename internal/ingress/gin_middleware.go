package ingress

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/proxy"
)

// responseWriterWrapper wraps an http.ResponseWriter to strip http.CloseNotifier.
// This prevents httputil.ReverseProxy from calling CloseNotify when the
// underlying writer does not support it (e.g. httptest.ResponseRecorder).
type responseWriterWrapper struct {
	http.ResponseWriter
}

// Middleware is a Gin middleware that intercepts ingress routes.
type Middleware struct {
	routeTable *RouteTable
	proxy      *proxy.Proxy
	selector   *BackendSelector
	limiter    *IngressLimiter
	logger     *slog.Logger
	enabled    bool
}

// MiddlewareOpts holds construction options.
type MiddlewareOpts struct {
	RouteTable *RouteTable
	Proxy      *proxy.Proxy
	Selector   *BackendSelector
	Limiter    *IngressLimiter
	Enabled    bool
}

func NewMiddleware(opts MiddlewareOpts) *Middleware {
	selector := opts.Selector
	if selector == nil {
		selector = NewBackendSelector()
	}
	limiter := opts.Limiter
	if limiter == nil {
		limiter = NewIngressLimiter()
	}
	return &Middleware{
		routeTable: opts.RouteTable,
		proxy:      opts.Proxy,
		selector:   selector,
		limiter:    limiter,
		logger:     slog.With("component", "ingress"),
		enabled:    opts.Enabled,
	}
}

// Handler returns the gin.HandlerFunc.
func (m *Middleware) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !m.enabled || m.routeTable == nil {
			c.Next()
			return
		}

		// Handle CORS preflight before routing.
		rule := m.routeTable.Lookup(c.Request)
		if rule != nil && rule.Annotations != nil && rule.Annotations.EnableCORS && c.Request.Method == http.MethodOptions {
			if c.Request.Header.Get("Access-Control-Request-Method") != "" {
				m.proxy.HandlePreflight(c.Writer, rule.Annotations, c.Request)
				c.Abort()
				return
			}
		}

		// SSL redirect.
		if rule != nil && rule.Annotations != nil && rule.Annotations.SSLRedirect && c.Request.TLS == nil {
			target := "https://" + c.Request.Host + c.Request.URL.RequestURI()
			c.Redirect(http.StatusMovedPermanently, target)
			c.Abort()
			return
		}

		if rule == nil || rule.BackendPool == nil {
			c.Next()
			return
		}

		// Rate limiting.
		if rule.Annotations != nil && (rule.Annotations.RateLimitRPS > 0 || rule.Annotations.RateLimitConnections > 0) {
			clientIP := c.ClientIP()
			if !m.limiter.Acquire(rule.ID, clientIP, rule.Annotations) {
				c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
				c.Abort()
				return
			}
			defer m.limiter.Release(rule.ID, clientIP)
		}

		// Select healthy backend.
		backends := rule.BackendPool.Healthy()
		if len(backends) == 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no healthy backends"})
			c.Abort()
			return
		}

		// Read affinity cookie before selection.
		var cookieVal string
		if rule.Annotations != nil && rule.Annotations.AffinityCookieName != "" {
			if cookie, err := c.Request.Cookie(rule.Annotations.AffinityCookieName); err == nil {
				cookieVal = cookie.Value
			}
		}

		// Select backend using selector.
		result, err := m.selector.Select(SelectionContext{
			Request:   c.Request,
			Rule:      rule,
			CookieVal: cookieVal,
		})
		if err != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
			c.Abort()
			return
		}
		backend := result.Backend
		if result.CookieName != "" {
			http.SetCookie(c.Writer, &http.Cookie{
				Name:  result.CookieName,
				Value: result.CookieVal,
				Path:  "/",
			})
		}

		// Body size limit.
		if rule.Annotations != nil && rule.Annotations.ProxyBodySize > 0 {
			if c.Request.ContentLength > rule.Annotations.ProxyBodySize {
				c.String(http.StatusRequestEntityTooLarge, "request entity too large")
				c.Abort()
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, rule.Annotations.ProxyBodySize)
		}

		m.logger.Debug("proxying ingress request",
			"host", c.Request.Host,
			"path", c.Request.URL.Path,
			"backend", backend.Name(),
			"address", backend.Address(),
		)

		if err := m.proxy.ServeHTTP(&responseWriterWrapper{ResponseWriter: c.Writer}, c.Request, rule, backend); err != nil {
			m.logger.Error("ingress proxy error", "error", err, "backend", backend.Name())
			// Only write error if response not yet written.
			if !c.Writer.Written() {
				c.JSON(http.StatusBadGateway, gin.H{"error": "bad gateway"})
			}
		}
		c.Abort()
	}
}
