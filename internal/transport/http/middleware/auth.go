package middleware

import (
	"net/http"
	"strings"

	"gateyes/internal/requestctx"
	"gateyes/internal/transport/http/handler"
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
