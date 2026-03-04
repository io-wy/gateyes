package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"gateyes/internal/requestctx"
)

func RequestContext() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get("x-request-id")
			if requestID == "" {
				requestID = newID("req")
			}

			sessionID := r.Header.Get("x-session-id")
			if sessionID == "" {
				sessionID = newID("sess")
			}

			w.Header().Set("x-request-id", requestID)
			w.Header().Set("x-session-id", sessionID)

			ctx := r.Context()
			ctx = requestctx.WithRequestID(ctx, requestID)
			ctx = requestctx.WithSessionID(ctx, sessionID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}
