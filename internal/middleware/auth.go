package middleware

import (
	"net/http"
	"strings"

	"gateyes/internal/handler"
	"gateyes/internal/requestctx"
)

func GatewayAuth(enabled bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled {
				// TODO(io): remove this bypass in production mode.
				next.ServeHTTP(w, r)
				return
			}

			token := readToken(r)
			if token == "" {
				handler.WriteError(w, http.StatusUnauthorized, "missing api key", handler.TypeAuthenticationError, "")
				return
			}

			// TODO(io): replace naive token passthrough with real token lookup + permission check.
			ctx := requestctx.WithTokenID(r.Context(), token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func AdminAuth(adminToken string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if adminToken == "" {
				// TODO(io): force admin auth once deployment topology is defined.
				next.ServeHTTP(w, r)
				return
			}

			token := readBearerToken(r.Header.Get("Authorization"))
			if token == "" || token != adminToken {
				handler.WriteError(w, http.StatusUnauthorized, "invalid admin token", handler.TypeAuthenticationError, "")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func readToken(r *http.Request) string {
	if key := strings.TrimSpace(r.Header.Get("x-api-key")); key != "" {
		return key
	}
	return readBearerToken(r.Header.Get("Authorization"))
}

func readBearerToken(authz string) string {
	parts := strings.SplitN(strings.TrimSpace(authz), " ", 2)
	if len(parts) != 2 {
		return ""
	}
	if !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
