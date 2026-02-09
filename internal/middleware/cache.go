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
			if provider == "" {
				provider = "default"
			}

			model, _ := reqData["model"].(string)
			messages := reqData["messages"]

			cacheKey, err := cm.cacheManager.GenerateKey(provider, model, messages)
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

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Cache", "HIT")
				w.WriteHeader(http.StatusOK)
				w.Write(cachedResponse)
				return
			}

			// Cache miss - capture response and cache it
			slog.Debug("cache miss", "key", cacheKey[:16]+"...")
			rw := &cacheResponseWriter{
				ResponseWriter: w,
				cacheManager:   cm.cacheManager,
				cacheKey:       cacheKey,
				ctx:            ctx,
			}

			next.ServeHTTP(rw, r)
		})
	}
}

// isCacheable checks if the request path is cacheable
func (cm *CacheMiddleware) isCacheable(path string) bool {
	cacheablePaths := []string{
		"/v1/chat/completions",
		"/v1/completions",
		"/v1/embeddings",
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
	body         bytes.Buffer
	statusCode   int
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
	// Cache the response if successful
	if crw.statusCode == 0 || crw.statusCode == http.StatusOK {
		if crw.body.Len() > 0 {
			err := crw.cacheManager.Set(crw.ctx, crw.cacheKey, crw.body.Bytes())
			if err != nil {
				slog.Error("failed to cache response", "error", err)
			} else {
				crw.ResponseWriter.Header().Set("X-Cache", "MISS")
			}
		}
	}

	if flusher, ok := crw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
