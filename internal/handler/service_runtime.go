package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

func (h *Handler) ServiceResponses(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceResponses)()

	rawBody := captureRequestBody(c)

	var req provider.ResponseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.RecordError(metricsSurfaceResponses, "", metricsResultClientError, "invalid_request")
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	req.Normalize()

	identity, ok := middleware.Identity(c)
	if !ok {
		h.metrics.RecordError(metricsSurfaceResponses, "", metricsResultAuthError, "invalid_api_key")
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid API key")
		return
	}

	cacheTrace := &responseSvc.CacheTrace{}
	reqCtx := responseSvc.WithGatewayHintsFromHeaders(c.Request.Context(), c.GetHeader)
	reqCtx = responseSvc.WithRawRequestBody(reqCtx, rawBody)
	reqCtx = responseSvc.WithCacheTrace(reqCtx, cacheTrace)
	if req.Stream {
		stream, _, err := h.catalog.CreateStream(reqCtx, identity, c.Param("prefix"), "responses", &req, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceErrorV2(c, metricsSurfaceResponses, "", err)
			return
		}
		attachCacheHeaders(c, cacheTrace)
		h.streamResponses(c, stream, req.Model, start)
		return
	}

	result, _, err := h.catalog.Create(reqCtx, identity, c.Param("prefix"), "responses", &req, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceErrorV2(c, metricsSurfaceResponses, "", err)
		return
	}
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceResponses, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	h.logRequestCompleted(c, metricsSurfaceResponses, result.ProviderName, http.StatusOK, time.Since(start))
	attachCacheHeaders(c, cacheTrace)
	writeOK(c, result.Response)
}

func (h *Handler) ServiceChat(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceChatCompletions)()

	rawBody := captureRequestBody(c)

	var req provider.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.RecordError(metricsSurfaceChatCompletions, "", metricsResultClientError, "invalid_request")
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	identity, ok := middleware.Identity(c)
	if !ok {
		h.metrics.RecordError(metricsSurfaceChatCompletions, "", metricsResultAuthError, "invalid_api_key")
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid API key")
		return
	}

	responseReq := provider.ConvertChatRequest(&req)
	cacheTrace := &responseSvc.CacheTrace{}
	reqCtx := responseSvc.WithGatewayHintsFromHeaders(c.Request.Context(), c.GetHeader)
	reqCtx = responseSvc.WithRawRequestBody(reqCtx, rawBody)
	reqCtx = responseSvc.WithCacheTrace(reqCtx, cacheTrace)
	if req.Stream {
		stream, _, err := h.catalog.CreateStream(reqCtx, identity, c.Param("prefix"), "chat", responseReq, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceErrorV2(c, metricsSurfaceChatCompletions, "", err)
			return
		}
		attachCacheHeaders(c, cacheTrace)
		h.streamChatCompatibility(c, stream, req.Model, start)
		return
	}
	result, _, err := h.catalog.Create(reqCtx, identity, c.Param("prefix"), "chat", responseReq, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceErrorV2(c, metricsSurfaceChatCompletions, "", err)
		return
	}
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceChatCompletions, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	h.logRequestCompleted(c, metricsSurfaceChatCompletions, result.ProviderName, http.StatusOK, time.Since(start))
	attachCacheHeaders(c, cacheTrace)
	writeOK(c, provider.ConvertResponseToChat(result.Response))
}

func (h *Handler) ServiceMessages(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceMessages)()

	rawBody := captureRequestBody(c)

	var req provider.AnthropicMessagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.RecordError(metricsSurfaceMessages, "", metricsResultClientError, "invalid_request")
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	identity, ok := middleware.Identity(c)
	if !ok {
		h.metrics.RecordError(metricsSurfaceMessages, "", metricsResultAuthError, "invalid_api_key")
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid API key")
		return
	}

	responseReq := provider.ConvertAnthropicRequest(&req)
	cacheTrace := &responseSvc.CacheTrace{}
	reqCtx := responseSvc.WithGatewayHintsFromHeaders(c.Request.Context(), c.GetHeader)
	reqCtx = responseSvc.WithRawRequestBody(reqCtx, rawBody)
	reqCtx = responseSvc.WithCacheTrace(reqCtx, cacheTrace)
	if req.Stream {
		stream, _, err := h.catalog.CreateStream(reqCtx, identity, c.Param("prefix"), "messages", responseReq, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceErrorV2(c, metricsSurfaceMessages, "", err)
			return
		}
		attachCacheHeaders(c, cacheTrace)
		h.streamAnthropicMessages(c, stream, req.Model, start)
		return
	}
	result, _, err := h.catalog.Create(reqCtx, identity, c.Param("prefix"), "messages", responseReq, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceErrorV2(c, metricsSurfaceMessages, "", err)
		return
	}
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceMessages, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	h.logRequestCompleted(c, metricsSurfaceMessages, result.ProviderName, http.StatusOK, time.Since(start))
	attachCacheHeaders(c, cacheTrace)
	writeOK(c, provider.ConvertResponseToAnthropic(result.Response))
}

func (h *Handler) ServiceInvoke(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceResponses)()

	rawBody := captureRequestBody(c)

	var req catalog.PromptInvokeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.RecordError(metricsSurfaceResponses, "", metricsResultClientError, "invalid_request")
		writeError(c, http.StatusBadRequest, CodeInvalidRequestBody, err.Error())
		return
	}
	identity, ok := middleware.Identity(c)
	if !ok {
		h.metrics.RecordError(metricsSurfaceResponses, "", metricsResultAuthError, "invalid_api_key")
		writeError(c, http.StatusUnauthorized, CodeInvalidAPIKey, "invalid API key")
		return
	}

	cacheTrace := &responseSvc.CacheTrace{}
	reqCtx := responseSvc.WithGatewayHintsFromHeaders(c.Request.Context(), c.GetHeader)
	reqCtx = responseSvc.WithRawRequestBody(reqCtx, rawBody)
	reqCtx = responseSvc.WithCacheTrace(reqCtx, cacheTrace)
	if req.Stream {
		stream, _, err := h.catalog.CreatePromptInvocationStream(reqCtx, identity, c.Param("prefix"), req, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceErrorV2(c, metricsSurfaceResponses, "", err)
			return
		}
		attachCacheHeaders(c, cacheTrace)
		h.streamResponses(c, stream, "", start)
		return
	}

	result, _, err := h.catalog.CreatePromptInvocation(reqCtx, identity, c.Param("prefix"), req, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceErrorV2(c, metricsSurfaceResponses, "", err)
		return
	}
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceResponses, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	h.logRequestCompleted(c, metricsSurfaceResponses, result.ProviderName, http.StatusOK, time.Since(start))
	attachCacheHeaders(c, cacheTrace)
	writeOK(c, result.Response)
}
