package middleware

import "github.com/gin-gonic/gin"

const (
	metricsSurfaceResponses       = "responses"
	metricsSurfaceChatCompletions = "chat_completions"
	metricsSurfaceMessages        = "messages"
	metricsSurfaceModels          = "models"
	metricsSurfaceAdmin           = "admin"

	metricsResultClientError = "client_error"
	metricsResultAuthError   = "auth_error"
	metricsResultRateLimited = "rate_limited"
)

func recordMiddlewareError(metrics MetricsRecorder, c *gin.Context, result, errorClass string) {
	if metrics == nil || c == nil {
		return
	}
	metrics.RecordError(surfaceFromRequest(c), "", result, errorClass)
}

func surfaceFromRequest(c *gin.Context) string {
	path := c.FullPath()
	if path == "" && c.Request != nil {
		path = c.Request.URL.Path
	}
	switch {
	case path == "/v1/responses" || path == "/v1/responses/:id":
		return metricsSurfaceResponses
	case path == "/v1/chat/completions":
		return metricsSurfaceChatCompletions
	case path == "/v1/messages":
		return metricsSurfaceMessages
	case path == "/v1/models":
		return metricsSurfaceModels
	case path == "/admin" || path == "/admin/*path":
		return metricsSurfaceAdmin
	case len(path) >= 6 && path[:6] == "/admin":
		return metricsSurfaceAdmin
	default:
		return metricsSurfaceAdmin
	}
}
