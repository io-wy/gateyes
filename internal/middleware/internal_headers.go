package middleware

import (
	"net/http"

	"gateyes/internal/requestmeta"
)

func SanitizeInternalHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clearInternalHeaders(r)
			next.ServeHTTP(w, r)
		})
	}
}

func clearInternalHeaders(r *http.Request) {
	if r == nil {
		return
	}

	r.Header.Del(requestmeta.HeaderVirtualKey)
	r.Header.Del(requestmeta.HeaderResolvedProvider)
	r.Header.Del(requestmeta.HeaderResolvedModel)
	r.Header.Del(requestmeta.HeaderUsagePromptTokens)
	r.Header.Del(requestmeta.HeaderUsageCompletionTokens)
	r.Header.Del(requestmeta.HeaderUsageTotalTokens)
	r.Header.Del(requestmeta.HeaderUsageEstimatedTokens)
	r.Header.Del(requestmeta.HeaderStreamRequest)
	r.Header.Del(requestmeta.HeaderRetryCount)
	r.Header.Del(requestmeta.HeaderFallbackCount)
	r.Header.Del(requestmeta.HeaderCircuitOpenCount)
	r.Header.Del(requestmeta.HeaderCacheStatus)
}
