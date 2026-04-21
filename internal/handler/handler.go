package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/middleware"
	"github.com/gateyes/gateway/internal/repository"
	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

type Handler struct {
	cfg       *config.Config
	deps      *Dependencies
	responses *responseSvc.Service
	catalog   *catalog.Service
	metrics   *Metrics
	logger    *slog.Logger
}

type Dependencies struct {
	Config      *config.Config
	Store       repository.Store
	Metrics     *Metrics
	ProviderMgr *provider.Manager
	ResponseSvc *responseSvc.Service
	CatalogSvc  *catalog.Service
}

func NewHandler(deps *Dependencies) *Handler {
	return &Handler{
		cfg:       deps.Config,
		deps:      deps,
		responses: deps.ResponseSvc,
		catalog:   deps.CatalogSvc,
		metrics:   deps.Metrics,
		logger:    slog.With("component", "handler"),
	}
}

func (h *Handler) requestLogger(c *gin.Context) *slog.Logger {
	logger := h.logger
	if logger == nil {
		logger = slog.Default().With("component", "handler")
	}
	if requestCtx, ok := middleware.GetRequestContext(c); ok && requestCtx != nil {
		logger = logger.With(
			"request_id", requestCtx.RequestID,
			"trace_id", requestCtx.TraceID,
		)
	}
	return logger
}

func (h *Handler) logRequestCompleted(c *gin.Context, surface, providerName string, status int, latency time.Duration) {
	h.requestLogger(c).Info("request completed",
		"surface", surface,
		"provider", normalizeMetricsProvider(providerName),
		"status", status,
		"latency_ms", latency.Milliseconds(),
	)
}

func (h *Handler) logRequestFailed(c *gin.Context, surface, providerName string, status int, err error) {
	h.requestLogger(c).Error("request failed",
		"surface", surface,
		"provider", normalizeMetricsProvider(providerName),
		"status", status,
		"error", err,
	)
}

func (h *Handler) Chat(c *gin.Context) {
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
	h.logRequestCompleted(c, metricsSurfaceChatCompletions, result.ProviderName, http.StatusOK, time.Since(start))
	c.JSON(http.StatusOK, provider.ConvertResponseToChat(result.Response))
}

func (h *Handler) Responses(c *gin.Context) {
	h.handleResponsesCreate(c)
}

func (h *Handler) AnthropicMessages(c *gin.Context) {
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
	h.logRequestCompleted(c, metricsSurfaceMessages, result.ProviderName, http.StatusOK, time.Since(start))
	c.JSON(http.StatusOK, provider.ConvertResponseToAnthropic(result.Response))
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
		if len(identity.APIKeyProviders) > 0 && !stringInSlice(identity.APIKeyProviders, p.Name()) {
			continue
		}
		if len(identity.APIKeyModels) > 0 && !stringInSlice(identity.APIKeyModels, p.Model()) {
			continue
		}
		record, hasRegistry := h.deps.ProviderMgr.Registry(p.Name())
		if hasRegistry && !matchesModelFilters(c, record) {
			continue
		}
		models = append(models, map[string]any{
			"id":       p.Model(),
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": p.Name(),
			"provider": p.Name(),
			"capabilities": map[string]any{
				"chat":              hasRegistry && record.SupportsChat,
				"responses":         hasRegistry && record.SupportsResponses,
				"messages":          hasRegistry && record.SupportsMessages,
				"stream":            !hasRegistry || record.SupportsStream,
				"tools":             !hasRegistry || record.SupportsTools,
				"images":            !hasRegistry || record.SupportsImages,
				"structured_output": !hasRegistry || record.SupportsStructuredOutput,
				"long_context":      hasRegistry && record.SupportsLongContext,
			},
			"health_status":  registryString(hasRegistry, record.HealthStatus, "unknown"),
			"enabled":        !hasRegistry || record.Enabled,
			"drain":          hasRegistry && record.Drain,
			"routing_weight": registryInt(hasRegistry, record.RoutingWeight),
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
	wrapped := responseSvc.WrapError(err)
	httpErr := gatewayHTTPError{Status: wrapped.Status, Message: wrapped.Message, Type: wrapped.Type}
	switch {
	case errors.Is(err, catalog.ErrServiceNotPublished), errors.Is(err, catalog.ErrServiceDisabled), errors.Is(err, catalog.ErrServiceSurfaceDenied), errors.Is(err, catalog.ErrPromptTemplateMissing), errors.Is(err, catalog.ErrPromptVariableMissing), errors.Is(err, catalog.ErrPolicyViolation), errors.Is(err, catalog.ErrRateLimited), errors.Is(err, catalog.ErrServiceAccessDenied):
		httpErr = h.wrapCatalogError(err)
	}
	status := httpErr.Status
	if status >= http.StatusInternalServerError {
		status = h.inferHTTPStatus(err)
	}

	result, errorClass := classifyMetricsError(err, httpErr.Type)
	h.metrics.RecordError(surface, providerName, result, errorClass)
	h.logRequestFailed(c, surface, providerName, status, err)

	c.JSON(status, gin.H{"error": gin.H{"message": httpErr.Message, "type": httpErr.Type}})
}

type gatewayHTTPError struct {
	Status  int
	Message string
	Type    string
}

func (h *Handler) wrapCatalogError(err error) gatewayHTTPError {
	switch {
	case errors.Is(err, catalog.ErrServiceAccessDenied):
		return gatewayHTTPError{Status: 403, Message: err.Error(), Type: "invalid_request_error"}
	case errors.Is(err, catalog.ErrRateLimited):
		return gatewayHTTPError{Status: 429, Message: err.Error(), Type: "rate_limit_error"}
	case errors.Is(err, catalog.ErrServiceNotPublished), errors.Is(err, catalog.ErrServiceDisabled), errors.Is(err, catalog.ErrServiceSurfaceDenied), errors.Is(err, catalog.ErrPromptTemplateMissing), errors.Is(err, catalog.ErrPromptVariableMissing), errors.Is(err, catalog.ErrPolicyViolation):
		return gatewayHTTPError{Status: 400, Message: err.Error(), Type: "invalid_request_error"}
	default:
		return gatewayHTTPError{Status: 500, Message: err.Error(), Type: "internal_error"}
	}
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

func matchesModelFilters(c *gin.Context, record repository.ProviderRegistryRecord) bool {
	if providerName := c.Query("provider"); providerName != "" && providerName != record.Name {
		return false
	}
	if health := c.Query("health_status"); health != "" && health != record.HealthStatus {
		return false
	}
	if surface := c.Query("surface"); surface != "" {
		switch surface {
		case "chat":
			if !record.SupportsChat {
				return false
			}
		case "responses":
			if !record.SupportsResponses {
				return false
			}
		case "messages":
			if !record.SupportsMessages {
				return false
			}
		}
	}
	if value, ok := queryBool(c, "stream"); ok && record.SupportsStream != value {
		return false
	}
	if value, ok := queryBool(c, "tools"); ok && record.SupportsTools != value {
		return false
	}
	if value, ok := queryBool(c, "images"); ok && record.SupportsImages != value {
		return false
	}
	if value, ok := queryBool(c, "structured_output"); ok && record.SupportsStructuredOutput != value {
		return false
	}
	if value, ok := queryBool(c, "long_context"); ok && record.SupportsLongContext != value {
		return false
	}
	return true
}

func queryBool(c *gin.Context, key string) (bool, bool) {
	raw := c.Query(key)
	if raw == "" {
		return false, false
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return value, true
}

func stringInSlice(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func registryString(ok bool, value, fallback string) string {
	if !ok || value == "" {
		return fallback
	}
	return value
}

func registryInt(ok bool, value int) int {
	if !ok {
		return 0
	}
	return value
}
