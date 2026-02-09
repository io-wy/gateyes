package gateway

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"gateyes/internal/mcp"
)

// StaticProxyWithGuard wraps a static proxy with MCP protection
type StaticProxyWithGuard struct {
	upstream    *url.URL
	stripPrefix string
	proxy       *httputil.ReverseProxy
	mcpGuard    *mcp.MCPGuard
	guardConfig mcp.GuardConfig
}

// NewStaticProxyWithGuard creates a new static proxy with MCP guard
func NewStaticProxyWithGuard(upstreamURL, stripPrefix string, guardConfig mcp.GuardConfig) (*StaticProxyWithGuard, error) {
	u, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream URL: %w", err)
	}

	sp := &StaticProxyWithGuard{
		upstream:    u,
		stripPrefix: stripPrefix,
		guardConfig: guardConfig,
	}

	// Initialize MCP guard if enabled
	if guardConfig.Enabled {
		sp.mcpGuard = mcp.NewMCPGuard(guardConfig)
		if err := sp.mcpGuard.RegisterMCP(upstreamURL); err != nil {
			return nil, fmt.Errorf("failed to register MCP: %w", err)
		}
		slog.Info("MCP guard enabled for static proxy",
			"upstream", upstreamURL,
			"health_check", guardConfig.HealthCheck.Enabled,
		)
	}

	// Create reverse proxy
	sp.proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = u.Scheme
			req.URL.Host = u.Host
			if sp.stripPrefix != "" {
				req.URL.Path = strings.TrimPrefix(req.URL.Path, sp.stripPrefix)
			}
			req.Host = u.Host
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("proxy error",
				"upstream", upstreamURL,
				"error", err,
			)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}

	return sp, nil
}

// ServeHTTP handles the request with MCP protection
func (sp *StaticProxyWithGuard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if sp.mcpGuard == nil || !sp.guardConfig.Enabled {
		// No guard, proxy directly
		sp.proxy.ServeHTTP(w, r)
		return
	}

	// Execute with MCP guard protection
	ctx := r.Context()
	err := sp.mcpGuard.ExecuteRequest(ctx, sp.upstream.String(), func(client *http.Client) error {
		// Create a custom transport that uses the provided client
		originalTransport := sp.proxy.Transport
		sp.proxy.Transport = client.Transport

		// Capture response
		rw := &responseWriter{ResponseWriter: w}
		sp.proxy.ServeHTTP(rw, r)

		// Restore original transport
		sp.proxy.Transport = originalTransport

		// Check if request was successful
		if rw.statusCode >= 500 {
			return fmt.Errorf("upstream returned status %d", rw.statusCode)
		}

		return nil
	})

	if err != nil {
		slog.Error("MCP request failed",
			"upstream", sp.upstream.String(),
			"error", err,
		)
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}
}

// GetMetrics returns MCP metrics
func (sp *StaticProxyWithGuard) GetMetrics() map[string]interface{} {
	if sp.mcpGuard == nil {
		return nil
	}
	return sp.mcpGuard.GetMetrics(sp.upstream.String())
}

// Close cleans up resources
func (sp *StaticProxyWithGuard) Close() error {
	if sp.mcpGuard != nil {
		return sp.mcpGuard.Close()
	}
	return nil
}

// responseWriter captures the response status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	if !rw.written {
		rw.statusCode = statusCode
		rw.written = true
	}
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}
