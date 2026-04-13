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
		stream, err := h.responses.CreateStream(c.Request.Context(), identity, &req, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, metricsSurfaceResponses, "", err)
			return
		}
		h.streamResponses(c, stream, req.Model, start)
		return
	}

	result, err := h.responses.Create(c.Request.Context(), identity, &req, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, metricsSurfaceResponses, "", err)
		return
	}

	// upstreamLatency = total latency - (retry delays)
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceResponses, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	h.logRequestCompleted(c, metricsSurfaceResponses, result.ProviderName, http.StatusOK, time.Since(start))
	c.JSON(http.StatusOK, result.Response)
}

func (h *Handler) streamResponses(c *gin.Context, stream *responseSvc.Stream, requestedModel string, start time.Time) {
	defer h.metrics.TrackStream(metricsSurfaceResponses)()

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

	for {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				// Stream ended
				h.metrics.ObserveStreamDuration(metricsSurfaceResponses, stream.ProviderName, metricsResultSuccess, time.Since(start))
				h.logRequestCompleted(c, metricsSurfaceResponses, stream.ProviderName, http.StatusOK, time.Since(start))
				writeSSEDone(c)
				flusher.Flush()
				return
			}

			normalizedEvents := normalizeResponsesStreamEvent(event)
			for _, normalized := range normalizedEvents {
				if !firstTokenRecorded && normalizedEventType(normalized) == "response.output_text.delta" {
					h.metrics.ObserveTTFT(metricsSurfaceResponses, stream.ProviderName, time.Since(start))
					firstTokenRecorded = true
				}
				if err := writeSSE(c, normalized); err != nil {
					return
				}
				flusher.Flush()
			}
			if event.Type == provider.EventResponseCompleted && event.Response != nil {
				h.observeResponse(metricsSurfaceResponses, stream.ProviderName, event.Response.Usage, time.Since(start))
			}
		case err, ok := <-stream.Errors:
			if ok && err != nil {
				result, errorClass := classifyMetricsError(err, "internal_error")
				h.metrics.RecordError(metricsSurfaceResponses, stream.ProviderName, result, errorClass)
				h.metrics.ObserveStreamDuration(metricsSurfaceResponses, stream.ProviderName, result, time.Since(start))
				h.logRequestFailed(c, metricsSurfaceResponses, stream.ProviderName, http.StatusBadGateway, err)
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

func normalizeResponsesStreamEvent(event provider.ResponseEvent) []provider.ResponseEvent {
	switch event.Type {
	case provider.EventContentDelta:
		var normalized []provider.ResponseEvent
		if event.Text() != "" {
			textEvent := event
			textEvent.Type = "response.output_text.delta"
			textEvent.Delta = event.Text()
			textEvent.TextDelta = ""
			textEvent.ToolCalls = nil
			normalized = append(normalized, textEvent)
		}
		for _, call := range event.ToolCalls {
			output := provider.ResponseOutput{
				ID:     call.ID,
				Type:   "function_call",
				Status: "completed",
				CallID: call.ID,
				Name:   call.Function.Name,
				Args:   call.Function.Arguments,
			}
			normalized = append(normalized, provider.ResponseEvent{
				Type:   "response.output_item.done",
				Output: &output,
			})
		}
		return normalized
	case provider.EventResponseStarted:
		started := event
		started.Type = "response.created"
		return []provider.ResponseEvent{started}
	case provider.EventResponseCompleted:
		completed := event
		completed.Type = "response.completed"
		return []provider.ResponseEvent{completed}
	case provider.EventToolCallDone:
		done := event
		done.Type = "response.output_item.done"
		return []provider.ResponseEvent{done}
	case provider.EventThinkingDelta:
		return nil
	default:
		return []provider.ResponseEvent{event}
	}
}

func normalizedEventType(event provider.ResponseEvent) string {
	return event.Type
}

func (h *Handler) streamChatCompatibility(c *gin.Context, stream *responseSvc.Stream, model string, start time.Time) {
	defer h.metrics.TrackStream(metricsSurfaceChatCompletions)()

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
	encoder := apicompat.NewChatStreamEncoder(stream.ResponseID, model)

	for {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				// Stream ended
				h.metrics.ObserveStreamDuration(metricsSurfaceChatCompletions, stream.ProviderName, metricsResultSuccess, time.Since(start))
				h.logRequestCompleted(c, metricsSurfaceChatCompletions, stream.ProviderName, http.StatusOK, time.Since(start))
				writeSSEDone(c)
				flusher.Flush()
				return
			}

			// 记录首个 token 延迟
			if !firstTokenRecorded && event.Type == provider.EventContentDelta {
				h.metrics.ObserveTTFT(metricsSurfaceChatCompletions, stream.ProviderName, time.Since(start))
				firstTokenRecorded = true
			}

			for _, chunk := range encoder.Encode(event) {
				if err := writeSSE(c, chunk); err != nil {
					return
				}
				flusher.Flush()
			}
			if event.Type == provider.EventResponseCompleted && event.Response != nil {
				h.observeResponse(metricsSurfaceChatCompletions, stream.ProviderName, event.Response.Usage, time.Since(start))
			}
		case err, ok := <-stream.Errors:
			if ok && err != nil {
				result, errorClass := classifyMetricsError(err, "internal_error")
				h.metrics.RecordError(metricsSurfaceChatCompletions, stream.ProviderName, result, errorClass)
				h.metrics.ObserveStreamDuration(metricsSurfaceChatCompletions, stream.ProviderName, result, time.Since(start))
				h.logRequestFailed(c, metricsSurfaceChatCompletions, stream.ProviderName, http.StatusBadGateway, err)
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
	defer h.metrics.TrackStream(metricsSurfaceMessages)()

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
	encoder := apicompat.NewAnthropicStreamEncoder(stream.ResponseID, model)

	for {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				// Stream ended
				h.metrics.ObserveStreamDuration(metricsSurfaceMessages, stream.ProviderName, metricsResultSuccess, time.Since(start))
				h.logRequestCompleted(c, metricsSurfaceMessages, stream.ProviderName, http.StatusOK, time.Since(start))
				writeSSEDone(c)
				flusher.Flush()
				return
			}

			// 记录首个 token 延迟
			if !firstTokenRecorded && event.Type == provider.EventContentDelta {
				h.metrics.ObserveTTFT(metricsSurfaceMessages, stream.ProviderName, time.Since(start))
				firstTokenRecorded = true
			}

			for _, anthropicEvent := range encoder.Encode(event) {
				if err := writeSSE(c, anthropicEvent); err != nil {
					return
				}
				flusher.Flush()
			}
			if event.Type == provider.EventResponseCompleted && event.Response != nil {
				h.observeResponse(metricsSurfaceMessages, stream.ProviderName, event.Response.Usage, time.Since(start))
			}
		case err, ok := <-stream.Errors:
			if ok && err != nil {
				result, errorClass := classifyMetricsError(err, "internal_error")
				h.metrics.RecordError(metricsSurfaceMessages, stream.ProviderName, result, errorClass)
				h.metrics.ObserveStreamDuration(metricsSurfaceMessages, stream.ProviderName, result, time.Since(start))
				h.logRequestFailed(c, metricsSurfaceMessages, stream.ProviderName, http.StatusBadGateway, err)
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
