package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"gateyes/internal/config"
)

type quotaEntry struct {
	count   int
	resetAt time.Time
}

type Quota struct {
	mu      sync.Mutex
	entries map[string]*quotaEntry
	config  config.QuotaConfig
	auth    config.AuthConfig
	now     func() time.Time
}

func NewQuota(cfg config.QuotaConfig, auth config.AuthConfig) *Quota {
	return &Quota{
		entries: make(map[string]*quotaEntry),
		config:  cfg,
		auth:    auth,
		now:     time.Now,
	}
}

func (q *Quota) Middleware() Middleware {
	if !q.config.Enabled {
		return Noop()
	}

	skip := make(map[string]struct{})
	for _, path := range q.config.SkipPaths {
		skip[path] = struct{}{}
	}

	window := q.config.Window.Duration
	if window <= 0 {
		window = 24 * time.Hour
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if _, ok := skip[req.URL.Path]; ok {
				next.ServeHTTP(w, req)
				return
			}

			key := quotaKey(req, q.config.By, q.config.Header, q.auth)
			if !q.allow(key, window) {
				http.Error(w, "quota exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, req)
		})
	}
}

func (q *Quota) allow(key string, window time.Duration) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := q.now()
	entry, ok := q.entries[key]
	if !ok || now.After(entry.resetAt) {
		q.entries[key] = &quotaEntry{
			count:   1,
			resetAt: now.Add(window),
		}
		return true
	}

	if entry.count >= q.config.Requests {
		return false
	}
	entry.count++
	return true
}

func quotaKey(req *http.Request, by string, header string, auth config.AuthConfig) string {
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
