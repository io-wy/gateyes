package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/protocol/apicompat"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

func (h *Handler) GetResponse(c *gin.Context) {
	identity, ok := middleware.Identity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	record, err := h.deps.Store.GetResponse(c.Request.Context(), identity.TenantID, c.Param("id"))
	if err != nil {
		if err == repository.ErrNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "response not found", "type": "invalid_request_error"}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}
	if len(record.ResponseBody) == 0 {
		c.JSON(http.StatusOK, gin.H{"id": record.ID, "status": record.Status})
		return
	}

	c.Data(http.StatusOK, "application/json", record.ResponseBody)
}

func (h *Handler) handleResponsesCreate(c *gin.Context) {
	start := time.Now()
	h.metrics.inflightRequests.Inc()
	defer h.metrics.inflightRequests.Dec()

	var req provider.ResponseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.errors.WithLabelValues(req.Model, "invalid_request").Inc()
		h.metrics.modelFailures.WithLabelValues(req.Model).Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}
	req.Normalize()

	identity, ok := middleware.Identity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	if req.Stream {
		stream, err := h.responses.CreateStream(c.Request.Context(), identity, &req, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, req.Model, err)
			return
		}
		h.streamResponses(c, stream, req.Model, start)
		return
	}

	result, err := h.responses.Create(c.Request.Context(), identity, &req, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, req.Model, err)
		return
	}

	if result.CacheHit {
		h.metrics.cacheHits.Inc()
	} else if h.cfg.Cache.Enabled {
		h.metrics.cacheMisses.Inc()
	}

	// upstreamLatency = total latency - (retry delays)
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(req.Model, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	c.JSON(http.StatusOK, result.Response)
}

func (h *Handler) streamResponses(c *gin.Context, stream *responseSvc.Stream, requestedModel string, start time.Time) {
	h.metrics.activeStreams.Inc()
	defer h.metrics.activeStreams.Dec()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "streaming not supported", "type": "internal_error"}})
		return
	}

	firstTokenRecorded := false
	modelLabel := requestedModel
	if stream.ProviderName != "" {
		modelLabel = stream.ProviderName
	}

	for {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				// Stream ended
				h.metrics.streamDuration.WithLabelValues(modelLabel).Observe(time.Since(start).Seconds())
				writeSSEDone(c)
				flusher.Flush()
				return
			}

			// 记录首个 token 延迟
			if !firstTokenRecorded && (event.Type == "response.output_text.delta" || event.Type == "chat.delta") {
				h.metrics.timeToFirstToken.WithLabelValues(modelLabel).Observe(time.Since(start).Seconds())
				firstTokenRecorded = true
			}

			if err := writeSSE(c, event); err != nil {
				return
			}
			flusher.Flush()
			if event.Type == "response.completed" && event.Response != nil {
				h.observeResponse(requestedModel, stream.ProviderName, event.Response.Usage, time.Since(start), false)
			}
		case err, ok := <-stream.Errors:
			if ok && err != nil {
				// 记录错误
				h.metrics.errors.WithLabelValues(requestedModel, "stream_error").Inc()
				h.metrics.modelFailures.WithLabelValues(requestedModel).Inc()
				_ = writeSSE(c, gin.H{"type": "error", "message": err.Error()})
				writeSSEDone(c)
				flusher.Flush()
				return
			}
		case <-c.Request.Context().Done():
			return
		}
	}
}

func (h *Handler) streamChatCompatibility(c *gin.Context, stream *responseSvc.Stream, model string, start time.Time) {
	h.metrics.activeStreams.Inc()
	defer h.metrics.activeStreams.Dec()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "streaming not supported", "type": "internal_error"}})
		return
	}

	firstTokenRecorded := false
	modelLabel := model
	if stream.ProviderName != "" {
		modelLabel = stream.ProviderName
	}
	encoder := apicompat.NewChatStreamEncoder(stream.ResponseID, model)

	for {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				// Stream ended
				h.metrics.streamDuration.WithLabelValues(modelLabel).Observe(time.Since(start).Seconds())
				writeSSEDone(c)
				flusher.Flush()
				return
			}

			// 记录首个 token 延迟
			if !firstTokenRecorded && (event.Type == "response.output_text.delta" || event.Type == "chat.delta" || event.Type == "chat.completion.chunk") {
				h.metrics.timeToFirstToken.WithLabelValues(modelLabel).Observe(time.Since(start).Seconds())
				firstTokenRecorded = true
			}

			for _, chunk := range encoder.Encode(event) {
				if err := writeSSE(c, chunk); err != nil {
					return
				}
				flusher.Flush()
			}
			if event.Type == "response.completed" && event.Response != nil {
				h.observeResponse(model, stream.ProviderName, event.Response.Usage, time.Since(start), false)
			}
		case err, ok := <-stream.Errors:
			if ok && err != nil {
				// 记录错误
				h.metrics.errors.WithLabelValues(model, "stream_error").Inc()
				h.metrics.modelFailures.WithLabelValues(model).Inc()
				_ = writeSSE(c, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
				writeSSEDone(c)
				flusher.Flush()
				return
			}
		case <-c.Request.Context().Done():
			return
		}
	}
}

func (h *Handler) streamAnthropicMessages(c *gin.Context, stream *responseSvc.Stream, model string, start time.Time) {
	h.metrics.activeStreams.Inc()
	defer h.metrics.activeStreams.Dec()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "streaming not supported", "type": "internal_error"}})
		return
	}

	firstTokenRecorded := false
	modelLabel := model
	if stream.ProviderName != "" {
		modelLabel = stream.ProviderName
	}
	encoder := apicompat.NewAnthropicStreamEncoder(stream.ResponseID, model)

	for {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				// Stream ended
				h.metrics.streamDuration.WithLabelValues(modelLabel).Observe(time.Since(start).Seconds())
				writeSSEDone(c)
				flusher.Flush()
				return
			}

			// 记录首个 token 延迟
			if !firstTokenRecorded && (event.Type == "response.output_text.delta" || event.Type == "chat.delta") {
				h.metrics.timeToFirstToken.WithLabelValues(modelLabel).Observe(time.Since(start).Seconds())
				firstTokenRecorded = true
			}

			for _, anthropicEvent := range encoder.Encode(event) {
				if err := writeSSE(c, anthropicEvent); err != nil {
					return
				}
				flusher.Flush()
			}
			if event.Type == "response.completed" && event.Response != nil {
				h.observeResponse(model, stream.ProviderName, event.Response.Usage, time.Since(start), false)
			}
		case err, ok := <-stream.Errors:
			if ok && err != nil {
				// 记录错误
				h.metrics.errors.WithLabelValues(model, "stream_error").Inc()
				h.metrics.modelFailures.WithLabelValues(model).Inc()
				_ = writeSSE(c, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
				writeSSEDone(c)
				flusher.Flush()
				return
			}
		case <-c.Request.Context().Done():
			return
		}
	}
}
