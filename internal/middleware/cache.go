package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"gateyes/internal/cache"
	"gateyes/internal/requestmeta"
)

// CacheMiddleware provides response caching for LLM requests
type CacheMiddleware struct {
	cacheManager *cache.CacheManager
	config       cache.CacheConfig
}

// NewCacheMiddleware creates a new cache middleware
func NewCacheMiddleware(config cache.CacheConfig) (*CacheMiddleware, error) {
	manager, err := cache.NewCacheManager(config)
	if err != nil {
		return nil, err
	}

	return &CacheMiddleware{
		cacheManager: manager,
		config:       config,
	}, nil
}

// Middleware returns the HTTP middleware handler
func (cm *CacheMiddleware) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cm.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Only cache POST requests to chat completions
			if r.Method != http.MethodPost || !cm.isCacheable(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Read request body
			body, err := io.ReadAll(r.Body)
			if err != nil {
				slog.Error("failed to read request body for caching", "error", err)
				next.ServeHTTP(w, r)
				return
			}
			r.Body = io.NopCloser(bytes.NewBuffer(body))

			// Parse request to extract cache key parameters
			var reqData map[string]interface{}
			if err := json.Unmarshal(body, &reqData); err != nil {
				slog.Error("failed to parse request for caching", "error", err)
				next.ServeHTTP(w, r)
				return
			}

			// Generate cache key
			provider := r.Header.Get("X-Gateyes-Provider")
			if provider == "" {
				provider = r.URL.Query().Get("provider")
			}
			virtualKey := strings.TrimSpace(r.Header.Get(requestmeta.HeaderVirtualKey))
			if virtualKey != "" {
				if provider == "" {
					provider = "vk:" + virtualKey
				} else {
					provider = provider + "|vk:" + virtualKey
				}
			}
			if provider == "" {
				provider = "default"
			}

			model, _ := reqData["model"].(string)
			messages := reqData["messages"]
			if stream, ok := reqData["stream"].(bool); ok && stream {
				r.Header.Set(requestmeta.HeaderCacheStatus, "BYPASS")
				next.ServeHTTP(w, r)
				return
			}

			cachePayload := buildCachePayload(messages, reqData)
			cacheKey, err := cm.cacheManager.GenerateKey(provider, model, cachePayload)
			if err != nil {
				slog.Error("failed to generate cache key", "error", err)
				next.ServeHTTP(w, r)
				return
			}

			// Try to get from cache
			ctx := r.Context()
			cachedResponse, hit, err := cm.cacheManager.Get(ctx, cacheKey)
			if err != nil {
				slog.Error("cache get error", "error", err)
			}

			if hit {
				// Cache hit - return cached response
				slog.Info("cache hit",
					"provider", provider,
					"model", model,
					"key", cacheKey[:16]+"...",
				)

				r.Header.Set(requestmeta.HeaderResolvedProvider, provider)
				r.Header.Set(requestmeta.HeaderResolvedModel, model)
				r.Header.Set(requestmeta.HeaderCacheStatus, "HIT")
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Cache", "HIT")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(cachedResponse)
				return
			}

			// Cache miss - capture response and cache it
			slog.Debug("cache miss", "key", cacheKey[:16]+"...")
			r.Header.Set(requestmeta.HeaderResolvedProvider, provider)
			r.Header.Set(requestmeta.HeaderResolvedModel, model)
			rw := &cacheResponseWriter{
				ResponseWriter: w,
				cacheManager:   cm.cacheManager,
				cacheKey:       cacheKey,
				ctx:            ctx,
				request:        r,
			}

			next.ServeHTTP(rw, r)
			rw.finalize()
		})
	}
}

// isCacheable checks if the request path is cacheable
func (cm *CacheMiddleware) isCacheable(path string) bool {
	cacheablePaths := []string{
		"/v1/chat/completions",
		"/v1/completions",
	}

	for _, p := range cacheablePaths {
		if strings.Contains(path, p) {
			return true
		}
	}

	return false
}

// GetStats returns cache statistics
func (cm *CacheMiddleware) GetStats() cache.CacheStats {
	return cm.cacheManager.GetStats()
}

// Close cleans up cache resources
func (cm *CacheMiddleware) Close() error {
	return cm.cacheManager.Close()
}

// cacheResponseWriter wraps http.ResponseWriter to cache responses
type cacheResponseWriter struct {
	http.ResponseWriter
	cacheManager *cache.CacheManager
	cacheKey     string
	ctx          context.Context
	request      *http.Request
	body         bytes.Buffer
	statusCode   int
	cached       bool
}

func (crw *cacheResponseWriter) Write(b []byte) (int, error) {
	// Capture response body
	crw.body.Write(b)
	return crw.ResponseWriter.Write(b)
}

func (crw *cacheResponseWriter) WriteHeader(statusCode int) {
	crw.statusCode = statusCode
	crw.ResponseWriter.WriteHeader(statusCode)
}

func (crw *cacheResponseWriter) Flush() {
	if flusher, ok := crw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (crw *cacheResponseWriter) finalize() {
	if crw.cached {
		return
	}
	crw.cached = true

	status := crw.statusCode
	if status == 0 {
		status = http.StatusOK
	}

	if status != http.StatusOK || crw.body.Len() == 0 {
		if crw.request != nil {
			crw.request.Header.Set(requestmeta.HeaderCacheStatus, "BYPASS")
		}
		return
	}

	if isStreamingContentType(crw.ResponseWriter.Header().Get("Content-Type")) {
		if crw.request != nil {
			crw.request.Header.Set(requestmeta.HeaderCacheStatus, "BYPASS")
		}
		return
	}

	if err := crw.cacheManager.Set(crw.ctx, crw.cacheKey, crw.body.Bytes()); err != nil {
		slog.Error("failed to cache response", "error", err)
		if crw.request != nil {
			crw.request.Header.Set(requestmeta.HeaderCacheStatus, "MISS")
		}
		return
	}

	if crw.request != nil {
		crw.request.Header.Set(requestmeta.HeaderCacheStatus, "MISS")
	}
	crw.ResponseWriter.Header().Set("X-Cache", "MISS")
}

func buildCachePayload(messages interface{}, reqData map[string]interface{}) map[string]interface{} {
	payload := map[string]interface{}{
		"messages": messages,
	}

	optionalKeys := []string{
		"temperature",
		"top_p",
		"max_tokens",
		"max_completion_tokens",
	}
	for _, key := range optionalKeys {
		if value, ok := reqData[key]; ok {
			payload[key] = value
		}
	}
	return payload
}

func isStreamingContentType(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

func (crw *cacheResponseWriter) cacheStatus() string {
	if crw.ResponseWriter.Header().Get("X-Cache") == "HIT" {
		return "HIT"
	}
	return "MISS"
}
