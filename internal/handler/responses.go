package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/middleware"
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
	req.Surface = "responses"

	identity, ok := middleware.Identity(c)
	if !ok {
		h.metrics.RecordError(metricsSurfaceResponses, "", metricsResultAuthError, "invalid_api_key")
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	hints := responseSvc.ParseCacheHintsFromHeaders(c.GetHeader)
	reqCtx := responseSvc.WithCacheHints(c.Request.Context(), hints)

	if req.Stream {
		stream, err := h.responses.CreateStream(reqCtx, identity, &req, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, metricsSurfaceResponses, "", err)
			return
		}
		h.streamResponses(c, stream, req.Model, start)
		return
	}

	result, err := h.responses.Create(reqCtx, identity, &req, c.GetHeader("X-Session-ID"))
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
	outputItemAdded := false
	var accumulatedText string
	outputItemID := "msg_" + stream.ResponseID[:8]

	for {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				// Stream ended — send done events if text was streamed but not closed
				if outputItemAdded {
					if err := writeCodexDoneEvents(c, accumulatedText, outputItemID); err != nil {
						return
					}
					flusher.Flush()
				}
				h.metrics.ObserveStreamDuration(metricsSurfaceResponses, stream.ProviderName, metricsResultSuccess, time.Since(start))
				h.logRequestCompleted(c, metricsSurfaceResponses, stream.ProviderName, http.StatusOK, time.Since(start))
				flusher.Flush()
				return
			}

			normalizedEvents := normalizeResponsesStreamEvent(event)
			for _, normalized := range normalizedEvents {
				if !firstTokenRecorded && normalizedEventType(normalized) == provider.EventContentDelta {
					h.metrics.ObserveTTFT(metricsSurfaceResponses, stream.ProviderName, time.Since(start))
					firstTokenRecorded = true
				}

				// Insert added events before the first output_text.delta
				if normalized.Type == "response.output_text.delta" && !outputItemAdded {
					if err := writeSSEEvent(c, "response.output_item.added", gin.H{
						"type":         "response.output_item.added",
						"output_index": 0,
						"item": gin.H{
							"id":      outputItemID,
							"type":    "message",
							"role":    "assistant",
							"status":  "in_progress",
							"content": []gin.H{},
						},
					}); err != nil {
						return
					}
					if err := writeSSEEvent(c, "response.content_part.added", gin.H{
						"type":          "response.content_part.added",
						"output_index":  0,
						"content_index": 0,
						"part": gin.H{
							"type": "output_text",
							"text": "",
						},
					}); err != nil {
						return
					}
					outputItemAdded = true
				}

				if normalized.Type == "response.output_text.delta" {
					accumulatedText += normalized.Delta
				}

				// Insert done events before response.completed
				if normalized.Type == "response.completed" && outputItemAdded {
					if err := writeCodexDoneEvents(c, accumulatedText, outputItemID); err != nil {
						return
					}
					outputItemAdded = false
				}

				// Write the event (response.created / response.completed need usage remap)
				if normalized.Response != nil && (normalized.Type == "response.created" || normalized.Type == "response.completed") {
					if err := writeCodexResponseEvent(c, normalized.Type, normalized.Response); err != nil {
						return
					}
				} else {
					if _, err := c.Writer.Write([]byte("event: " + normalized.Type + "\n")); err != nil {
						return
					}
					if err := writeSSE(c, normalized); err != nil {
						return
					}
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
				if _, werr := c.Writer.Write([]byte("event: error\n")); werr != nil {
					return
				}
				_ = writeSSE(c, gin.H{"type": "error", "message": err.Error()})
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
			zero := 0
			textEvent.OutputIndex = &zero
			textEvent.ContentIndex = &zero
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

func writeCodexDoneEvents(c *gin.Context, text string, itemID string) error {
	if err := writeSSEEvent(c, "response.output_text.done", gin.H{
		"type":          "response.output_text.done",
		"output_index":  0,
		"content_index": 0,
	}); err != nil {
		return err
	}
	if err := writeSSEEvent(c, "response.content_part.done", gin.H{
		"type":          "response.content_part.done",
		"output_index":  0,
		"content_index": 0,
		"part": gin.H{
			"type": "output_text",
			"text": text,
		},
	}); err != nil {
		return err
	}
	if err := writeSSEEvent(c, "response.output_item.done", gin.H{
		"type":         "response.output_item.done",
		"output_index": 0,
		"item": gin.H{
			"id":      itemID,
			"type":    "message",
			"role":    "assistant",
			"status":  "completed",
			"content": []gin.H{{"type": "output_text", "text": text}},
		},
	}); err != nil {
		return err
	}
	return nil
}

func writeCodexResponseEvent(c *gin.Context, eventType string, resp *provider.Response) error {
	payload := map[string]any{
		"type": eventType,
		"response": map[string]any{
			"id":         resp.ID,
			"object":     resp.Object,
			"created_at": resp.Created,
			"model":      resp.Model,
			"status":     resp.Status,
			"output":     resp.Output,
			"usage": map[string]any{
				"input_tokens":  resp.Usage.PromptTokens,
				"output_tokens": resp.Usage.CompletionTokens,
				"total_tokens":  resp.Usage.TotalTokens,
				"input_tokens_details": map[string]any{
					"cached_tokens": resp.Usage.CachedTokens,
				},
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = c.Writer.Write([]byte("event: " + eventType + "\ndata: " + string(data) + "\n\n"))
	return err
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
	encoder := provider.NewChatStreamEncoder(stream.ResponseID, model)

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
	encoder := provider.NewAnthropicStreamEncoder(stream.ResponseID, model)

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
				if _, err := c.Writer.Write([]byte("event: " + anthropicEvent.Type + "\n")); err != nil {
					return
				}
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
