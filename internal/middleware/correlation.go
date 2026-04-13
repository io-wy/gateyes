package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/gin-gonic/gin"
)

func Correlation() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader(RequestIDHeader))
		if requestID == "" {
			requestID = generateHex(12)
		}

		traceparent := strings.TrimSpace(c.GetHeader(TraceparentHeader))
		traceID := parseTraceID(traceparent)
		if traceID == "" {
			traceID = generateHex(16)
			traceparent = buildTraceparent(traceID)
		}

		requestCtx := &RequestContext{
			RequestID:   requestID,
			TraceID:     traceID,
			Traceparent: traceparent,
		}
		SetRequestContext(c, requestCtx)
		c.Writer.Header().Set(RequestIDHeader, requestID)
		c.Writer.Header().Set(TraceparentHeader, traceparent)
		c.Next()
	}
}

func parseTraceID(traceparent string) string {
	parts := strings.Split(traceparent, "-")
	if len(parts) != 4 {
		return ""
	}
	if len(parts[1]) != 32 {
		return ""
	}
	return parts[1]
}

func buildTraceparent(traceID string) string {
	return "00-" + traceID + "-" + generateHex(8) + "-01"
}

func generateHex(size int) string {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat("0", size*2)
	}
	return hex.EncodeToString(buf)
}
