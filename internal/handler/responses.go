package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/auth"
	"github.com/gateyes/gateway/internal/service/provider"
)

func (h *Handler) GetResponse(c *gin.Context) {
	identity, ok := h.authIdentity(c)
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

	var req provider.ResponsesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	identity, ok := h.authIdentity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}
	if !h.authSvc.CheckModel(identity, req.Model) {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"message": auth.ErrModelNotAllowed.Error(), "type": "invalid_request_error"}})
		return
	}

	estimatedPromptTokens := h.estimateTokens(req.Messages)
	if !h.authSvc.HasQuota(identity, estimatedPromptTokens) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": auth.ErrQuotaExceeded.Error(), "type": "rate_limit_error"}})
		return
	}
	if !h.deps.Limiter.Allow(c.Request.Context(), identity.APIKey, estimatedPromptTokens) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": "rate limit exceeded", "type": "rate_limit_error"}})
		return
	}

	selected, err := h.selectProvider(c.Request.Context(), identity, c.GetHeader("X-Session-ID"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}
	if selected == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "no provider available", "type": "internal_error"}})
		return
	}

	responseID := uuid.NewString()
	requestBody, _ := json.Marshal(req)
	if err := h.deps.Store.CreateResponse(c.Request.Context(), repository.ResponseRecord{
		ID:           responseID,
		TenantID:     identity.TenantID,
		UserID:       identity.UserID,
		APIKeyID:     identity.APIKeyID,
		ProviderName: selected.Name(),
		Model:        req.Model,
		Status:       "in_progress",
		RequestBody:  requestBody,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}

	upstreamReq := &provider.ChatRequest{
		Model:     selected.Model(),
		Messages:  req.Messages,
		Stream:    req.Stream,
		MaxTokens: 0,
	}

	h.deps.Router.IncLoad(selected.Name())
	h.deps.ProviderMgr.Stats.IncrementLoad(selected.Name())
	defer func() {
		h.deps.Router.DecLoad(selected.Name())
		h.deps.ProviderMgr.Stats.DecrementLoad(selected.Name())
	}()

	if req.Stream {
		h.handleResponsesStream(c, identity, selected, req.Model, responseID, upstreamReq, estimatedPromptTokens, start)
		return
	}

	h.handleResponsesNormal(c, identity, selected, req.Model, responseID, upstreamReq, start)
}

func (h *Handler) handleResponsesNormal(c *gin.Context, identity *repository.AuthIdentity, p provider.Provider, requestedModel, responseID string, req *provider.ChatRequest, start time.Time) {
	resp, err := p.Chat(c.Request.Context(), req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		h.deps.ProviderMgr.Stats.RecordRequest(p.Name(), false, 0, latencyMs)
		_ = h.deps.Store.UpdateResponse(c.Request.Context(), repository.ResponseRecord{
			ID:           responseID,
			TenantID:     identity.TenantID,
			ProviderName: p.Name(),
			Model:        requestedModel,
			Status:       "error",
		})
		_ = h.authSvc.RecordUsage(c.Request.Context(), identity, p.Name(), requestedModel, 0, 0, 0, 0, latencyMs, "error", "upstream_error")
		c.JSON(http.StatusBadGateway, h.buildErrorResponse(err, p.Name()))
		return
	}

	response := provider.ConvertToResponses(resp)
	response.ID = responseID
	response.Model = requestedModel

	body, _ := json.Marshal(response)
	if err := h.deps.Store.UpdateResponse(c.Request.Context(), repository.ResponseRecord{
		ID:           responseID,
		TenantID:     identity.TenantID,
		ProviderName: p.Name(),
		Model:        requestedModel,
		Status:       "completed",
		ResponseBody: body,
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}

	if err := h.authSvc.RecordUsage(
		c.Request.Context(),
		identity,
		p.Name(),
		requestedModel,
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		resp.Usage.TotalTokens,
		p.Cost(resp.Usage.PromptTokens, resp.Usage.CompletionTokens),
		latencyMs,
		"success",
		"",
	); err != nil {
		if err == auth.ErrQuotaExceeded {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"message": err.Error(), "type": "rate_limit_error"}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}

	h.deps.ProviderMgr.Stats.RecordRequest(p.Name(), true, resp.Usage.TotalTokens, latencyMs)
	c.JSON(http.StatusOK, response)
}

func (h *Handler) handleResponsesStream(c *gin.Context, identity *repository.AuthIdentity, p provider.Provider, requestedModel, responseID string, req *provider.ChatRequest, estimatedPromptTokens int, start time.Time) {
	stream, errCh := p.ChatStream(c.Request.Context(), req)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "streaming not supported", "type": "internal_error"}})
		return
	}

	var assistantContent string
	for {
		select {
		case data, ok := <-stream:
			if !ok {
				completionTokens := roughTokenCount(assistantContent)
				response := &provider.ResponsesResponse{
					ID:      responseID,
					Object:  "response",
					Created: time.Now().Unix(),
					Model:   requestedModel,
					Choices: []provider.ResponsesChoice{{
						Index: 0,
						Message: provider.ChatMessage{
							Role:    "assistant",
							Content: assistantContent,
						},
						FinishReason: "stop",
					}},
					Usage: provider.Usage{
						PromptTokens:     estimatedPromptTokens,
						CompletionTokens: completionTokens,
						TotalTokens:      estimatedPromptTokens + completionTokens,
					},
				}
				body, _ := json.Marshal(response)
				latencyMs := time.Since(start).Milliseconds()
				_ = h.deps.Store.UpdateResponse(c.Request.Context(), repository.ResponseRecord{
					ID:           responseID,
					TenantID:     identity.TenantID,
					ProviderName: p.Name(),
					Model:        requestedModel,
					Status:       "completed",
					ResponseBody: body,
				})
				_ = h.authSvc.RecordUsage(
					c.Request.Context(),
					identity,
					p.Name(),
					requestedModel,
					estimatedPromptTokens,
					completionTokens,
					estimatedPromptTokens+completionTokens,
					p.Cost(estimatedPromptTokens, completionTokens),
					latencyMs,
					"success",
					"",
				)
				h.deps.ProviderMgr.Stats.RecordRequest(p.Name(), true, estimatedPromptTokens+completionTokens, latencyMs)
				c.Writer.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
				return
			}

			assistantContent += extractDeltaContent(data)
			c.Writer.Write([]byte("data: " + data + "\n\n"))
			flusher.Flush()

		case err := <-errCh:
			if err != nil {
				latencyMs := time.Since(start).Milliseconds()
				_ = h.deps.Store.UpdateResponse(c.Request.Context(), repository.ResponseRecord{
					ID:           responseID,
					TenantID:     identity.TenantID,
					ProviderName: p.Name(),
					Model:        requestedModel,
					Status:       "error",
				})
				_ = h.authSvc.RecordUsage(c.Request.Context(), identity, p.Name(), requestedModel, 0, 0, 0, 0, latencyMs, "error", "upstream_error")
				h.deps.ProviderMgr.Stats.RecordRequest(p.Name(), false, 0, latencyMs)
				c.JSON(http.StatusBadGateway, h.buildErrorResponse(err, p.Name()))
				return
			}
		case <-c.Request.Context().Done():
			return
		}
	}
}

func (h *Handler) selectProvider(ctx context.Context, identity *repository.AuthIdentity, sessionID string) (provider.Provider, error) {
	providers, err := h.allowedProviders(ctx, identity)
	if err != nil {
		return nil, err
	}
	return h.deps.Router.SelectFrom(providers, sessionID), nil
}

func (h *Handler) allowedProviders(ctx context.Context, identity *repository.AuthIdentity) ([]provider.Provider, error) {
	providerNames, err := h.deps.Store.ListTenantProviders(ctx, identity.TenantID)
	if err != nil {
		return nil, err
	}
	return h.deps.ProviderMgr.ListByNames(providerNames), nil
}

func extractDeltaContent(data string) string {
	var payload struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return ""
	}
	if len(payload.Choices) == 0 {
		return ""
	}
	return payload.Choices[0].Delta.Content
}

func roughTokenCount(content string) int {
	if content == "" {
		return 0
	}
	return len(content) / 4
}
