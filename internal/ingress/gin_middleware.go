package ingress

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/proxy"
)

// Middleware is a Gin middleware that intercepts ingress routes.
type Middleware struct {
	routeTable  *RouteTable
	proxy       *proxy.Proxy
	logger      *slog.Logger
	enabled     bool
}

// MiddlewareOpts holds construction options.
type MiddlewareOpts struct {
	RouteTable *RouteTable
	Proxy      *proxy.Proxy
	Enabled    bool
}

func NewMiddleware(opts MiddlewareOpts) *Middleware {
	return &Middleware{
		routeTable: opts.RouteTable,
		proxy:      opts.Proxy,
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

		// Select healthy backend.
		backends := rule.BackendPool.Healthy()
		if len(backends) == 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no healthy backends"})
			c.Abort()
			return
		}

		// For MVP use first healthy backend; later integrate Router strategy.
		backend := backends[0]

		// Body size limit.
		if rule.Annotations != nil && rule.Annotations.ProxyBodySize > 0 {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, rule.Annotations.ProxyBodySize)
		}

		m.logger.Debug("proxying ingress request",
			"host", c.Request.Host,
			"path", c.Request.URL.Path,
			"backend", backend.Name(),
			"address", backend.Address(),
		)

		if err := m.proxy.ServeHTTP(c.Writer, c.Request, rule, backend); err != nil {
			m.logger.Error("ingress proxy error", "error", err, "backend", backend.Name())
			// Only write error if response not yet written.
			if !c.Writer.Written() {
				c.JSON(http.StatusBadGateway, gin.H{"error": "bad gateway"})
			}
		}
		c.Abort()
	}
}
