package gateway

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type UpstreamProxy struct {
	rest *httputil.ReverseProxy
	ws   *websocketProxy
}

func NewUpstreamProxy(baseURL, wsBaseURL string, headers map[string]string, authHeader, authScheme, apiKey, stripPrefix string) (*UpstreamProxy, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("base url is required")
	}

	target, err := parseHTTPURL(baseURL)
	if err != nil {
		return nil, err
	}

	wsURL, err := parseWSURL(wsBaseURL, target)
	if err != nil {
		return nil, err
	}

	authHeader, authScheme = normalizeAuth(authHeader, authScheme, apiKey)

	rest := buildReverseProxy(target, headers, authHeader, authScheme, apiKey, stripPrefix)
	wsProxy := newWebSocketProxy(wsURL, headers, authHeader, authScheme, apiKey, stripPrefix)

	return &UpstreamProxy{rest: rest, ws: wsProxy}, nil
}

func (p *UpstreamProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		if p.ws == nil || p.ws.baseURL == nil {
			http.Error(w, "websocket upstream not configured", http.StatusBadGateway)
			return
		}
		p.ws.ServeHTTP(w, r)
		return
	}

	p.rest.ServeHTTP(w, r)
}

func parseHTTPURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid base url %q: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid base url %q", raw)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("base url must be http or https: %q", raw)
	}
	return parsed, nil
}

func parseWSURL(raw string, base *url.URL) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return deriveWSURL(base), nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid websocket url %q: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid websocket url %q", raw)
	}
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		return deriveWSURL(parsed), nil
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return nil, fmt.Errorf("websocket url must be ws or wss: %q", raw)
	}
	return parsed, nil
}

func deriveWSURL(base *url.URL) *url.URL {
	wsURL := *base
	switch base.Scheme {
	case "https":
		wsURL.Scheme = "wss"
	case "http":
		wsURL.Scheme = "ws"
	}
	return &wsURL
}

func buildReverseProxy(target *url.URL, headers map[string]string, authHeader, authScheme, apiKey, stripPrefix string) *httputil.ReverseProxy {
	director := func(req *http.Request) {
		path := stripPath(req.URL.Path, stripPrefix)
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = joinURLPath(target.Path, path)
		req.Host = target.Host
		req.URL.RawPath = ""
		applyHeaders(req.Header, headers, authHeader, authScheme, apiKey)
	}

	proxy := &httputil.ReverseProxy{
		Director:      director,
		FlushInterval: 50 * time.Millisecond,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          512,
			MaxIdleConnsPerHost:   128,
			MaxConnsPerHost:       256,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("reverse proxy error", "error", err, "path", r.URL.Path)
			http.Error(w, "upstream error", http.StatusBadGateway)
		},
	}

	return proxy
}

func stripPath(path, prefix string) string {
	if prefix == "" || prefix == "/" {
		return path
	}
	if !strings.HasPrefix(path, prefix) {
		return path
	}
	trimmed := strings.TrimPrefix(path, prefix)
	if trimmed == "" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	return trimmed
}

func joinURLPath(basePath, reqPath string) string {
	if reqPath == "" {
		return basePath
	}
	if basePath == "" || basePath == "/" {
		return reqPath
	}
	return strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(reqPath, "/")
}

func normalizeAuth(authHeader, authScheme, apiKey string) (string, string) {
	if apiKey == "" {
		return authHeader, authScheme
	}
	if authHeader == "" {
		authHeader = "Authorization"
	}
	if authScheme == "" && strings.EqualFold(authHeader, "Authorization") {
		authScheme = "Bearer"
	}
	return authHeader, authScheme
}

func applyHeaders(header http.Header, extra map[string]string, authHeader, authScheme, apiKey string) {
	if apiKey != "" {
		value := apiKey
		if authScheme != "" {
			value = authScheme + " " + apiKey
		}
		header.Set(authHeader, value)
	}
	for key, value := range extra {
		if strings.TrimSpace(key) == "" {
			continue
		}
		header.Set(key, value)
	}
}

type websocketProxy struct {
	baseURL     *url.URL
	headers     map[string]string
	authHeader  string
	authScheme  string
	apiKey      string
	stripPrefix string
	dialer      websocket.Dialer
	upgrader    websocket.Upgrader
}

func newWebSocketProxy(baseURL *url.URL, headers map[string]string, authHeader, authScheme, apiKey, stripPrefix string) *websocketProxy {
	return &websocketProxy{
		baseURL:     baseURL,
		headers:     headers,
		authHeader:  authHeader,
		authScheme:  authScheme,
		apiKey:      apiKey,
		stripPrefix: stripPrefix,
		dialer: websocket.Dialer{
			Proxy:            http.ProxyFromEnvironment,
			HandshakeTimeout: 10 * time.Second,
		},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (p *websocketProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.baseURL == nil {
		http.Error(w, "websocket upstream not configured", http.StatusBadGateway)
		return
	}

	upstreamURL := *p.baseURL
	upstreamURL.Path = joinURLPath(p.baseURL.Path, stripPath(r.URL.Path, p.stripPrefix))
	upstreamURL.RawQuery = r.URL.RawQuery

	headers := cloneHeaders(r.Header)
	sanitizeWSHeaders(headers)
	applyHeaders(headers, p.headers, p.authHeader, p.authScheme, p.apiKey)

	upstreamConn, resp, err := p.dialer.DialContext(r.Context(), upstreamURL.String(), headers)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		slog.Error("websocket dial failed", "error", err, "url", upstreamURL.String())
		http.Error(w, "websocket upstream unavailable", http.StatusBadGateway)
		return
	}
	defer upstreamConn.Close()

	responseHeader := http.Header{}
	if resp != nil {
		if protocol := resp.Header.Get("Sec-WebSocket-Protocol"); protocol != "" {
			responseHeader.Set("Sec-WebSocket-Protocol", protocol)
		}
	}

	clientConn, err := p.upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}
	defer clientConn.Close()

	errc := make(chan error, 2)
	go pipeWebSocket(errc, clientConn, upstreamConn)
	go pipeWebSocket(errc, upstreamConn, clientConn)

	err = <-errc
	if err != nil && !isExpectedWSClose(err) {
		slog.Info("websocket proxy closed", "error", err)
	}
}

func cloneHeaders(original http.Header) http.Header {
	clone := make(http.Header, len(original))
	for key, values := range original {
		copyValues := make([]string, len(values))
		copy(copyValues, values)
		clone[key] = copyValues
	}
	return clone
}

func sanitizeWSHeaders(header http.Header) {
	header.Del("Connection")
	header.Del("Upgrade")
	header.Del("Sec-Websocket-Key")
	header.Del("Sec-Websocket-Version")
	header.Del("Sec-Websocket-Extensions")
	header.Del("Sec-Websocket-Accept")
	header.Del("Host")
}

func pipeWebSocket(errc chan<- error, dst, src *websocket.Conn) {
	for {
		messageType, message, err := src.ReadMessage()
		if err != nil {
			errc <- err
			return
		}
		if err := dst.WriteMessage(messageType, message); err != nil {
			errc <- err
			return
		}
	}
}

func isExpectedWSClose(err error) bool {
	if err == nil {
		return true
	}
	return websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure)
}
