// Package response contains protocol-level HTTP response helpers shared by
// public and administration transports.
package response

import (
	"encoding/json"
	"strconv"
	"strings"

	responseSvc "github.com/gateyes/gateway/internal/service/responses"
	"github.com/gin-gonic/gin"
)

// WriteError emits the stable JSON error envelope used by HTTP transports.
// Keeping this helper code-oriented (rather than depending on handler.Code)
// avoids an import cycle while allowing each bounded context to map its own
// domain errors.
func WriteError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

// WriteJSON writes a JSON response with an explicit content type and status.
func WriteJSON(c *gin.Context, status int, value any) { c.JSON(status, value) }

// AttachCacheHeaders emits the cache trace contract without coupling route
// registration or handlers to cache implementation details.
func AttachCacheHeaders(c *gin.Context, trace *responseSvc.CacheTrace) {
	if c == nil || trace == nil || trace.Result == "" {
		return
	}
	c.Header("X-Gateyes-Cache-Result", trace.Result)
	if trace.Layer != "" {
		c.Header("X-Gateyes-Cache-Layer", trace.Layer)
	}
	if trace.Reason != "" {
		c.Header("X-Gateyes-Cache-Reason", trace.Reason)
	}
	if trace.EntryID != "" {
		c.Header("X-Gateyes-Cache-Entry-ID", trace.EntryID)
	}
	if trace.Similarity > 0 {
		c.Header("X-Gateyes-Cache-Similarity", strconv.FormatFloat(trace.Similarity, 'f', 4, 64))
	}
	if trace.Threshold > 0 {
		c.Header("X-Gateyes-Cache-Threshold", strconv.FormatFloat(trace.Threshold, 'f', 4, 64))
	}
	if trace.EmbeddingModel != "" {
		c.Header("X-Gateyes-Cache-Embedding-Model", trace.EmbeddingModel)
	}
	if len(trace.Rewrites) > 0 {
		c.Header("X-Gateyes-Cache-Rewrites", strings.Join(trace.Rewrites, ","))
	}
	if trace.PromptCacheKey != "" {
		c.Header("X-Gateyes-Prompt-Cache-Key", trace.PromptCacheKey)
	}
}

// WriteSSE writes one standards-compliant server-sent event payload.
func WriteSSE(c *gin.Context, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = c.Writer.Write([]byte("data: " + string(data) + "\n\n"))
	return err
}

// WriteSSEEvent writes an event name and JSON payload.
func WriteSSEEvent(c *gin.Context, eventType string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = c.Writer.Write([]byte("event: " + eventType + "\ndata: " + string(data) + "\n\n"))
	return err
}

// WriteSSEDone emits the OpenAI-compatible stream terminator.
func WriteSSEDone(c *gin.Context) { _, _ = c.Writer.Write([]byte("data: [DONE]\n\n")) }
