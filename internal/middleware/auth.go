package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"gateyes/internal/config"
)

func Auth(cfg config.AuthConfig) Middleware {
	if !cfg.Enabled {
		return Noop()
	}

	keys := map[string]struct{}{}
	for _, key := range cfg.Keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		keys[key] = struct{}{}
	}
	if len(keys) == 0 {
		slog.Warn("auth enabled but no keys configured")
	}

	header := cfg.Header
	if header == "" {
		header = "Authorization"
	}
	queryParam := cfg.QueryParam
	if queryParam == "" {
		queryParam = "api_key"
	}

	skip := map[string]struct{}{}
	for _, path := range cfg.SkipPaths {
		skip[path] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := skip[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}

			token := extractToken(r, header, queryParam)
			if token == "" {
				http.Error(w, "missing auth token", http.StatusUnauthorized)
				return
			}
			if len(keys) > 0 {
				if _, ok := keys[token]; !ok {
					http.Error(w, "invalid auth token", http.StatusUnauthorized)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func extractToken(r *http.Request, header, queryParam string) string {
	if header != "" {
		value := r.Header.Get(header)
		if value != "" {
			lower := strings.ToLower(value)
			if strings.HasPrefix(lower, "bearer ") {
				return strings.TrimSpace(value[7:])
			}
			return value
		}
	}

	if queryParam != "" {
		return r.URL.Query().Get(queryParam)
	}

	return ""
}
