package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/provider"
)

func (h *Handler) ServiceResponses(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceResponses)()

	var req provider.ResponseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.RecordError(metricsSurfaceResponses, "", metricsResultClientError, "invalid_request")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	req.Normalize()

	identity, ok := middleware.Identity(c)
	if !ok {
		h.metrics.RecordError(metricsSurfaceResponses, "", metricsResultAuthError, "invalid_api_key")
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	if req.Stream {
		stream, _, err := h.catalog.CreateStream(c.Request.Context(), identity, c.Param("prefix"), "responses", &req, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, metricsSurfaceResponses, "", err)
			return
		}
		h.streamResponses(c, stream, req.Model, start)
		return
	}

	result, _, err := h.catalog.Create(c.Request.Context(), identity, c.Param("prefix"), "responses", &req, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, metricsSurfaceResponses, "", err)
		return
	}
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceResponses, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	h.logRequestCompleted(c, metricsSurfaceResponses, result.ProviderName, http.StatusOK, time.Since(start))
	c.JSON(http.StatusOK, result.Response)
}

func (h *Handler) ServiceChat(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceChatCompletions)()

	var req provider.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.RecordError(metricsSurfaceChatCompletions, "", metricsResultClientError, "invalid_request")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	identity, ok := middleware.Identity(c)
	if !ok {
		h.metrics.RecordError(metricsSurfaceChatCompletions, "", metricsResultAuthError, "invalid_api_key")
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	responseReq := provider.ConvertChatRequest(&req)
	if req.Stream {
		stream, _, err := h.catalog.CreateStream(c.Request.Context(), identity, c.Param("prefix"), "chat", responseReq, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, metricsSurfaceChatCompletions, "", err)
			return
		}
		h.streamChatCompatibility(c, stream, req.Model, start)
		return
	}
	result, _, err := h.catalog.Create(c.Request.Context(), identity, c.Param("prefix"), "chat", responseReq, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, metricsSurfaceChatCompletions, "", err)
		return
	}
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceChatCompletions, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	h.logRequestCompleted(c, metricsSurfaceChatCompletions, result.ProviderName, http.StatusOK, time.Since(start))
	c.JSON(http.StatusOK, provider.ConvertResponseToChat(result.Response))
}

func (h *Handler) ServiceMessages(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceMessages)()

	var req provider.AnthropicMessagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.RecordError(metricsSurfaceMessages, "", metricsResultClientError, "invalid_request")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	identity, ok := middleware.Identity(c)
	if !ok {
		h.metrics.RecordError(metricsSurfaceMessages, "", metricsResultAuthError, "invalid_api_key")
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	responseReq := provider.ConvertAnthropicRequest(&req)
	if req.Stream {
		stream, _, err := h.catalog.CreateStream(c.Request.Context(), identity, c.Param("prefix"), "messages", responseReq, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, metricsSurfaceMessages, "", err)
			return
		}
		h.streamAnthropicMessages(c, stream, req.Model, start)
		return
	}
	result, _, err := h.catalog.Create(c.Request.Context(), identity, c.Param("prefix"), "messages", responseReq, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, metricsSurfaceMessages, "", err)
		return
	}
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceMessages, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	h.logRequestCompleted(c, metricsSurfaceMessages, result.ProviderName, http.StatusOK, time.Since(start))
	c.JSON(http.StatusOK, provider.ConvertResponseToAnthropic(result.Response))
}

func (h *Handler) ServiceInvoke(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceResponses)()

	var req catalog.PromptInvokeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.RecordError(metricsSurfaceResponses, "", metricsResultClientError, "invalid_request")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	identity, ok := middleware.Identity(c)
	if !ok {
		h.metrics.RecordError(metricsSurfaceResponses, "", metricsResultAuthError, "invalid_api_key")
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	if req.Stream {
		stream, _, err := h.catalog.CreatePromptInvocationStream(c.Request.Context(), identity, c.Param("prefix"), req, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, metricsSurfaceResponses, "", err)
			return
		}
		h.streamResponses(c, stream, "", start)
		return
	}

	result, _, err := h.catalog.CreatePromptInvocation(c.Request.Context(), identity, c.Param("prefix"), req, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, metricsSurfaceResponses, "", err)
		return
	}
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceResponses, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	h.logRequestCompleted(c, metricsSurfaceResponses, result.ProviderName, http.StatusOK, time.Since(start))
	c.JSON(http.StatusOK, result.Response)
}
