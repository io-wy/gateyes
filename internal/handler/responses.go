package handler

import (
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

	var req provider.ResponseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.errors.WithLabelValues(req.Model, "invalid_request").Inc()
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
		h.streamResponses(c, stream, req.Model)
		return
	}

	result, err := h.responses.Create(c.Request.Context(), identity, &req, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, req.Model, err)
		return
	}

	if result.CacheHit {
		h.metrics.cacheHit.Inc()
	} else if h.cfg.Cache.Enabled {
		h.metrics.cacheMiss.Inc()
	}
	h.observeResponse(req.Model, result.ProviderName, result.Response.Usage, time.Since(start), result.CacheHit)
	c.JSON(http.StatusOK, result.Response)
}

func (h *Handler) streamResponses(c *gin.Context, stream *responseSvc.Stream, requestedModel string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "streaming not supported", "type": "internal_error"}})
		return
	}

	for {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				writeSSEDone(c)
				flusher.Flush()
				return
			}
			if err := writeSSE(c, event); err != nil {
				return
			}
			flusher.Flush()
			if event.Type == "response.completed" && event.Response != nil {
				h.observeResponse(requestedModel, stream.ProviderName, event.Response.Usage, time.Since(stream.StartedAt), false)
			}
		case err, ok := <-stream.Errors:
			if ok && err != nil {
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

func (h *Handler) streamChatCompatibility(c *gin.Context, stream *responseSvc.Stream, model string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "streaming not supported", "type": "internal_error"}})
		return
	}

	for {
		select {
		case event, ok := <-stream.Events:
			if !ok {
				writeSSEDone(c)
				flusher.Flush()
				return
			}

			chunk := provider.ConvertEventToChatChunk(stream.ResponseID, model, event)
			if chunk == nil {
				continue
			}
			if err := writeSSE(c, chunk); err != nil {
				return
			}
			flusher.Flush()
			if event.Type == "response.completed" && event.Response != nil {
				h.observeResponse(model, stream.ProviderName, event.Response.Usage, time.Since(stream.StartedAt), false)
			}
		case err, ok := <-stream.Errors:
			if ok && err != nil {
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
