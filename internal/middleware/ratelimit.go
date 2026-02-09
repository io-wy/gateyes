package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"gateyes/internal/config"

	"golang.org/x/time/rate"
)

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	config  config.RateLimitConfig
	auth    config.AuthConfig
	now     func() time.Time
	cleanup time.Duration
}

func NewRateLimiter(cfg config.RateLimitConfig, auth config.AuthConfig) *RateLimiter {
	return &RateLimiter{
		entries: make(map[string]*limiterEntry),
		config:  cfg,
		auth:    auth,
		now:     time.Now,
		cleanup: 10 * time.Minute,
	}
}

func (r *RateLimiter) Middleware() Middleware {
	if !r.config.Enabled {
		return Noop()
	}

	skip := make(map[string]struct{})
	for _, path := range r.config.SkipPaths {
		skip[path] = struct{}{}
	}

	burst := r.config.Burst
	if burst <= 0 {
		burst = r.config.RequestsPerMinute
	}

	limit := rate.Every(time.Minute / time.Duration(r.config.RequestsPerMinute))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if _, ok := skip[req.URL.Path]; ok {
				next.ServeHTTP(w, req)
				return
			}

			key := RequestKey(req, r.config.By, r.config.Header, r.auth)
			limiter := r.getLimiter(key, limit, burst)
			if !limiter.Allow() {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, req)
		})
	}
}

func (r *RateLimiter) getLimiter(key string, limit rate.Limit, burst int) *rate.Limiter {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	r.cleanupEntries(now)

	entry, ok := r.entries[key]
	if !ok {
		entry = &limiterEntry{limiter: rate.NewLimiter(limit, burst)}
		r.entries[key] = entry
	}
	entry.lastSeen = now
	return entry.limiter
}

func (r *RateLimiter) cleanupEntries(now time.Time) {
	for key, entry := range r.entries {
		if now.Sub(entry.lastSeen) > r.cleanup {
			delete(r.entries, key)
		}
	}
}

func RequestKey(req *http.Request, by string, header string, auth config.AuthConfig) string {
	mode := strings.ToLower(strings.TrimSpace(by))
	switch mode {
	case "header":
		if header == "" {
			return req.RemoteAddr
		}
		value := req.Header.Get(header)
		if value != "" {
			return value
		}
		return req.RemoteAddr
	case "ip":
		return req.RemoteAddr
	case "auth":
		fallthrough
	default:
		value := extractToken(req, auth.Header, auth.QueryParam)
		if value != "" {
			return value
		}
		return req.RemoteAddr
	}
}
