package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

func Logging() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := newResponseRecorder(w)
			start := time.Now()
			next.ServeHTTP(recorder, r)

			slog.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", recorder.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"bytes", recorder.bytes,
				"remote", r.RemoteAddr,
			)
		})
	}
}
