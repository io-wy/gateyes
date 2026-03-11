package upstream

import (
	"context"
	"errors"
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

type Proxy struct {
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

func New(
	baseURL string,
	wsBaseURL string,
	headers map[string]string,
	authHeader string,
	authScheme string,
	apiKey string,
	stripPrefix string,
) (*Proxy, error) {
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

	return &Proxy{
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

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

func (p *Proxy) ForwardRequest(
	r *http.Request,
	body []byte,
) (*http.Response, context.CancelFunc, error) {
	if p == nil || p.baseURL == nil {
		return nil, func() {}, errors.New("upstream not configured")
	}

	target := *p.baseURL
	target.Path = joinURLPath(p.baseURL.Path, stripPath(r.URL.Path, p.stripPrefix))
	target.RawQuery = r.URL.RawQuery

	ctx := r.Context()
	cancel := func() {}
	if p.requestTimeout > 0 && !isStreamingRequest(r) {
		ctx, cancel = context.WithTimeout(ctx, p.requestTimeout)
	}

	outReq := cloneRequestWithBody(r, body).WithContext(ctx)
	outReq.URL = &target
	outReq.Host = target.Host
	outReq.RequestURI = ""
	sanitizeHopHeaders(outReq.Header)
	sanitizeInternalHeaders(outReq.Header)
	applyHeaders(outReq.Header, p.headers, p.authHeader, p.authScheme, p.apiKey)

	resp, err := p.client.Do(outReq)
	if err != nil {
		return nil, cancel, err
	}
	return resp, cancel, nil
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

	client := &http.Client{Transport: transport}
	actual, loaded := sharedHTTPClients.LoadOrStore(key, client)
	if loaded {
		if shared, ok := actual.(*http.Client); ok {
			return shared
		}
	}
	return client
}

func (p *Proxy) serveREST(w http.ResponseWriter, r *http.Request) {
	resp, cancel, err := p.ForwardRequest(r, nil)
	defer cancel()
	if err != nil {
		if errors.Is(r.Context().Err(), context.Canceled) {
			return
		}
		if errors.Is(r.Context().Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
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
	if err := copyResponseBody(w, resp.Body, r.Context(), streaming); err != nil && !errors.Is(err, context.Canceled) {
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
