package middleware

import (
	"net/http"

	"gateyes/internal/config"
)

func Policy(cfg config.PolicyConfig) Middleware {
	if !cfg.Enabled {
		return Noop()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// TODO: add policy checks / audit hooks here.
			next.ServeHTTP(w, r)
		})
	}
}
