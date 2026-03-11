package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gateyes/internal/requestmeta"

	"github.com/gorilla/websocket"
)

type UpstreamProxy struct {
	baseURL        *url.URL
	headers        map[string]string
	authHeader     string
	authScheme     string
	apiKey         string
	stripPrefix    string
	requestTimeout time.Duration
	client         *http.Client
	ws             *websocketProxy
}

const defaultUpstreamRequestTimeout = 120 * time.Second

var sharedHTTPClients sync.Map

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

	client := getSharedHTTPClient(target)
	wsProxy := newWebSocketProxy(wsURL, headers, authHeader, authScheme, apiKey, stripPrefix)

	return &UpstreamProxy{
		baseURL:        target,
		headers:        headers,
		authHeader:     authHeader,
		authScheme:     authScheme,
		apiKey:         apiKey,
		stripPrefix:    stripPrefix,
		requestTimeout: defaultUpstreamRequestTimeout,
		client:         client,
		ws:             wsProxy,
	}, nil
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

	p.serveREST(w, r)
}

func getSharedHTTPClient(target *url.URL) *http.Client {
	key := target.Scheme + "://" + target.Host
	if cached, ok := sharedHTTPClients.Load(key); ok {
		if client, ok := cached.(*http.Client); ok {
			return client
		}
	}

	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          1024,
		MaxIdleConnsPerHost:   256,
		MaxConnsPerHost:       512,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
	}
	actual, loaded := sharedHTTPClients.LoadOrStore(key, client)
	if loaded {
		if shared, ok := actual.(*http.Client); ok {
			return shared
		}
	}
	return client
}

func (p *UpstreamProxy) serveREST(w http.ResponseWriter, r *http.Request) {
	if p.baseURL == nil {
		http.Error(w, "upstream not configured", http.StatusBadGateway)
		return
	}

	target := *p.baseURL
	target.Path = joinURLPath(p.baseURL.Path, stripPath(r.URL.Path, p.stripPrefix))
	target.RawQuery = r.URL.RawQuery

	ctx := r.Context()
	cancel := func() {}
	if p.requestTimeout > 0 && !isStreamingRequest(r) {
		ctx, cancel = context.WithTimeout(ctx, p.requestTimeout)
	}
	defer cancel()

	outReq := r.Clone(ctx)
	outReq.URL = &target
	outReq.Host = target.Host
	outReq.RequestURI = ""
	sanitizeHopHeaders(outReq.Header)
	sanitizeInternalHeaders(outReq.Header)
	applyHeaders(outReq.Header, p.headers, p.authHeader, p.authScheme, p.apiKey)

	resp, err := p.client.Do(outReq)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "upstream timeout", http.StatusGatewayTimeout)
			return
		}
		slog.Error("upstream request failed", "error", err, "path", r.URL.Path)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	streaming := isStreamingRequest(r) || isStreamingResponse(resp)
	if err := copyResponseBody(w, resp.Body, ctx, streaming); err != nil && !errors.Is(err, context.Canceled) {
		slog.Debug("upstream copy interrupted", "error", err, "path", r.URL.Path)
	}
}

func isStreamingRequest(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.Header.Get(requestmeta.HeaderStreamRequest)), "1") {
		return true
	}
	accept := strings.ToLower(strings.TrimSpace(r.Header.Get("Accept")))
	return strings.Contains(accept, "text/event-stream")
}

func isStreamingResponse(resp *http.Response) bool {
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/event-stream") {
		return true
	}
	for _, value := range resp.TransferEncoding {
		if strings.EqualFold(value, "chunked") {
			return true
		}
	}
	return false
}

func copyResponseBody(dst http.ResponseWriter, src io.Reader, ctx context.Context, flush bool) error {
	if !flush {
		_, err := io.Copy(dst, src)
		return err
	}

	flusher, ok := dst.(http.Flusher)
	if !ok {
		_, err := io.Copy(dst, src)
		return err
	}

	buffer := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := src.Read(buffer)
		if n > 0 {
			if _, writeErr := dst.Write(buffer[:n]); writeErr != nil {
				return writeErr
			}
			flusher.Flush()
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	sanitizeHopHeaders(src)
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func sanitizeInternalHeaders(header http.Header) {
	header.Del(requestmeta.HeaderVirtualKey)
	header.Del(requestmeta.HeaderResolvedProvider)
	header.Del(requestmeta.HeaderResolvedModel)
	header.Del(requestmeta.HeaderUsagePromptTokens)
	header.Del(requestmeta.HeaderUsageCompletionTokens)
	header.Del(requestmeta.HeaderUsageTotalTokens)
	header.Del(requestmeta.HeaderUsageEstimatedTokens)
	header.Del(requestmeta.HeaderStreamRequest)
	header.Del(requestmeta.HeaderRetryCount)
	header.Del(requestmeta.HeaderFallbackCount)
	header.Del(requestmeta.HeaderCircuitOpenCount)
	header.Del(requestmeta.HeaderCacheStatus)
}

func sanitizeHopHeaders(header http.Header) {
	removeConnectionHeaders(header)
	header.Del("Proxy-Connection")
	header.Del("Keep-Alive")
	header.Del("Proxy-Authenticate")
	header.Del("Proxy-Authorization")
	header.Del("Te")
	header.Del("Trailer")
	header.Del("Transfer-Encoding")
	header.Del("Upgrade")
}

func removeConnectionHeaders(header http.Header) {
	for _, connectionValue := range header.Values("Connection") {
		for _, token := range strings.Split(connectionValue, ",") {
			header.Del(strings.TrimSpace(token))
		}
	}
	header.Del("Connection")
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
