package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"gateyes/internal/config"
	"gateyes/internal/requestmeta"
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

	virtualKeys := make(map[string]config.VirtualKeyConfig)
	for key, virtualConfig := range cfg.VirtualKeys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if !virtualConfig.Enabled {
			continue
		}
		virtualKeys[key] = virtualConfig
	}

	if len(keys) == 0 && len(virtualKeys) == 0 {
		slog.Warn("auth enabled but no static or virtual keys configured")
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
			// Never trust user-provided internal routing metadata.
			r.Header.Del(requestmeta.HeaderVirtualKey)
			r.Header.Del(requestmeta.HeaderResolvedProvider)
			r.Header.Del(requestmeta.HeaderResolvedModel)
			r.Header.Del(requestmeta.HeaderUsagePromptTokens)
			r.Header.Del(requestmeta.HeaderUsageCompletionTokens)
			r.Header.Del(requestmeta.HeaderUsageTotalTokens)
			r.Header.Del(requestmeta.HeaderStreamRequest)

			if _, ok := skip[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}

			token := extractToken(r, header, queryParam)
			if token == "" {
				http.Error(w, "missing auth token", http.StatusUnauthorized)
				return
			}

			if _, ok := virtualKeys[token]; ok {
				r.Header.Set(requestmeta.HeaderVirtualKey, token)
				next.ServeHTTP(w, r)
				return
			}

			if len(keys) > 0 {
				if _, ok := keys[token]; ok {
					next.ServeHTTP(w, r)
					return
				}
			}

			if len(keys) > 0 || len(virtualKeys) > 0 {
				http.Error(w, "invalid auth token", http.StatusUnauthorized)
				return
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
