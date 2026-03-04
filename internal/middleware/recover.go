package middleware

import (
	"log"
	"net/http"

	"gateyes/internal/handler"
)

func RecoverJSON() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					log.Printf("panic recovered: request=%s panic=%v", r.URL.Path, p)
					handler.WriteError(w, http.StatusInternalServerError, "internal server error", handler.TypeInternalError, "")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
