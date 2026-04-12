package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/protocol/apicompat"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

type Handler struct {
	cfg       *config.Config
	deps      *Dependencies
	responses *responseSvc.Service
	metrics   *Metrics
	logger    *slog.Logger
}

type Dependencies struct {
	Config      *config.Config
	Store       repository.Store
	Metrics     *Metrics
	ProviderMgr *provider.Manager
	ResponseSvc *responseSvc.Service
}

func NewHandler(deps *Dependencies) *Handler {
	return &Handler{
		cfg:       deps.Config,
		deps:      deps,
		responses: deps.ResponseSvc,
		metrics:   deps.Metrics,
		logger:    slog.With("component", "handler"),
	}
}

func (h *Handler) Chat(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceChatCompletions)()

	var req apicompat.ChatCompletionRequest
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

	responseReq := apicompat.ConvertChatRequest(&req)
	if req.Stream {
		stream, err := h.responses.CreateStream(c.Request.Context(), identity, responseReq, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, metricsSurfaceChatCompletions, "", err)
			return
		}
		h.streamChatCompatibility(c, stream, req.Model, start)
		return
	}

	result, err := h.responses.Create(c.Request.Context(), identity, responseReq, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, metricsSurfaceChatCompletions, "", err)
		return
	}

	// upstreamLatency = total latency - (retry delays)
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceChatCompletions, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	c.JSON(http.StatusOK, apicompat.ConvertResponseToChat(result.Response))
}

func (h *Handler) Responses(c *gin.Context) {
	h.handleResponsesCreate(c)
}

func (h *Handler) AnthropicMessages(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceMessages)()

	var req apicompat.AnthropicMessagesRequest
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

	responseReq := apicompat.ConvertAnthropicRequest(&req)
	if req.Stream {
		stream, err := h.responses.CreateStream(c.Request.Context(), identity, responseReq, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, metricsSurfaceMessages, "", err)
			return
		}
		h.streamAnthropicMessages(c, stream, req.Model, start)
		return
	}

	result, err := h.responses.Create(c.Request.Context(), identity, responseReq, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, metricsSurfaceMessages, "", err)
		return
	}

	// upstreamLatency = total latency - (retry delays)
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceMessages, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	c.JSON(http.StatusOK, apicompat.ConvertResponseToAnthropic(result.Response))
}

func (h *Handler) Models(c *gin.Context) {
	identity, ok := middleware.Identity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	providerNames, err := h.deps.Store.ListTenantProviders(c.Request.Context(), identity.TenantID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}
	providers := h.deps.ProviderMgr.ListByNames(providerNames)
	models := make([]map[string]any, 0, len(providers))
	for _, p := range providers {
		models = append(models, map[string]any{
			"id":       p.Model(),
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": p.Name(),
			"provider": p.Name(),
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": models})
}

func (h *Handler) Metrics(c *gin.Context) {
	h.metrics.Handler().ServeHTTP(c.Writer, c.Request)
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) Ready(c *gin.Context) {
	if len(h.deps.ProviderMgr.List()) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no providers"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

func (h *Handler) observeResponse(surface, providerName string, usage provider.Usage, latency time.Duration) {
	h.metrics.RecordSuccess(surface, providerName, usage, latency, nil, 0, 0)
}

func (h *Handler) observeResponseWithUpstream(surface, providerName string, usage provider.Usage, latency, upstreamLatency time.Duration, retries, fallback int) {
	h.metrics.RecordSuccess(surface, providerName, usage, latency, &upstreamLatency, retries, fallback)
}

func (h *Handler) renderServiceError(c *gin.Context, surface, providerName string, err error) {
	httpErr := responseSvc.WrapError(err)
	status := httpErr.Status
	if status >= http.StatusInternalServerError {
		status = h.inferHTTPStatus(err)
	}

	result, errorClass := classifyMetricsError(err, httpErr.Type)
	h.metrics.RecordError(surface, providerName, result, errorClass)

	c.JSON(status, gin.H{"error": gin.H{"message": httpErr.Message, "type": httpErr.Type}})
}

func (h *Handler) inferHTTPStatus(err error) int {
	switch {
	case errors.Is(err, responseSvc.ErrNoProvider):
		return http.StatusServiceUnavailable
	case strings.Contains(err.Error(), "timeout"):
		return http.StatusGatewayTimeout
	case strings.Contains(err.Error(), "401"), strings.Contains(err.Error(), "authentication"):
		return http.StatusUnauthorized
	case strings.Contains(err.Error(), "403"), strings.Contains(err.Error(), "forbidden"):
		return http.StatusForbidden
	case strings.Contains(err.Error(), "429"), strings.Contains(err.Error(), "rate_limit"):
		return http.StatusTooManyRequests
	case strings.Contains(err.Error(), "400"), strings.Contains(err.Error(), "invalid"):
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

// SyncCircuitBreakerStates updates the circuit breaker state metrics
func (h *Handler) SyncCircuitBreakerStates() {
	states := h.responses.GetCircuitBreakerStates()
	for key, state := range states {
		// Parse key "tenantID:providerName"
		parts := strings.SplitN(key, ":", 2)
		if len(parts) == 2 {
			h.metrics.SetCircuitBreakerState(parts[0], parts[1], state)
		}
	}
}

func writeSSE(c *gin.Context, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = c.Writer.Write([]byte("data: " + string(data) + "\n\n"))
	return err
}

func writeSSEDone(c *gin.Context) {
	_, _ = c.Writer.Write([]byte("data: [DONE]\n\n"))
}
