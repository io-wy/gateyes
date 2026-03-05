package middleware

import (
	"net/http"

	logx "gateyes/internal/pkg/log"
	"gateyes/internal/transport/http/handler"
)

func RecoverJSON() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					logx.Errorf("panic recovered: request=%s panic=%v", r.URL.Path, p)
					handler.WriteError(w, http.StatusInternalServerError, "internal server error", handler.TypeInternalError, "")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
