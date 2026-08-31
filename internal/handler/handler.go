package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/handler/middleware"
	"github.com/gateyes/gateway/internal/ports"
	"github.com/gateyes/gateway/internal/service/catalog"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
	httpresponse "github.com/gateyes/gateway/internal/transport/http/response"
)

type Handler struct {
	cfg       *config.Config
	deps      *Dependencies
	responses ports.InferenceUseCase
	catalog   ports.CatalogUseCase
	batch     ports.BatchUseCase
	metrics   *Metrics
	logger    *slog.Logger
}

type Dependencies struct {
	Config      *config.Config
	Store       ports.AdminAccessPort
	Metrics     *Metrics
	ProviderMgr ports.ProviderCatalog
	ResponseSvc ports.InferenceUseCase
	CatalogSvc  ports.CatalogUseCase
	BatchSvc    ports.BatchUseCase
	RedisPing   func(ctx context.Context) error
}

func NewHandler(deps *Dependencies) *Handler {
	return &Handler{
		cfg:       deps.Config,
		deps:      deps,
		responses: deps.ResponseSvc,
		catalog:   deps.CatalogSvc,
		batch:     deps.BatchSvc,
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

func attachCacheHeaders(c *gin.Context, trace *responseSvc.CacheTrace) {
	httpresponse.AttachCacheHeaders(c, trace)
}

// captureRequestBody reads the raw request body from the gin context and
// restores it so that ShouldBindJSON can parse it afterwards.
func captureRequestBody(c *gin.Context) []byte {
	rawBody, _ := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))
	if len(rawBody) == 0 {
		rawBody, _ = c.GetRawData()
	}
	return rawBody
}

func (h *Handler) Chat(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceChatCompletions)()

	var req provider.ChatCompletionRequest
	rawBody := captureRequestBody(c)
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.RecordError(metricsSurfaceChatCompletions, "", metricsResultClientError, "invalid_request")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	identity, ok := h.requireIdentity(c, metricsSurfaceChatCompletions)
	if !ok {
		return
	}

	responseReq := provider.ConvertChatRequest(&req)
	cacheTrace := &responseSvc.CacheTrace{}
	reqCtx := responseSvc.WithGatewayHintsFromHeaders(c.Request.Context(), c.GetHeader)
	reqCtx = responseSvc.WithAdmissionChecked(reqCtx)
	reqCtx = responseSvc.WithRawRequestBody(reqCtx, rawBody)
	reqCtx = responseSvc.WithCacheTrace(reqCtx, cacheTrace)
	if req.Stream {
		stream, err := h.responses.CreateStream(reqCtx, identity, responseReq, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, metricsSurfaceChatCompletions, "", err)
			return
		}
		attachCacheHeaders(c, cacheTrace)
		h.streamChatCompatibility(c, stream, req.Model, start)
		return
	}

	result, err := h.responses.Create(reqCtx, identity, responseReq, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, metricsSurfaceChatCompletions, "", err)
		return
	}

	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceChatCompletions, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	h.logRequestCompleted(c, metricsSurfaceChatCompletions, result.ProviderName, http.StatusOK, time.Since(start))
	attachCacheHeaders(c, cacheTrace)
	c.JSON(http.StatusOK, provider.ConvertResponseToChat(result.Response))
}

func (h *Handler) Responses(c *gin.Context) {
	h.handleResponsesCreate(c)
}

func (h *Handler) AnthropicMessages(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceMessages)()

	var req provider.AnthropicMessagesRequest
	rawBody := captureRequestBody(c)
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.RecordError(metricsSurfaceMessages, "", metricsResultClientError, "invalid_request")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	identity, ok := h.requireIdentity(c, metricsSurfaceMessages)
	if !ok {
		return
	}

	responseReq := provider.ConvertAnthropicRequest(&req)
	cacheTrace := &responseSvc.CacheTrace{}
	reqCtx := responseSvc.WithGatewayHintsFromHeaders(c.Request.Context(), c.GetHeader)
	reqCtx = responseSvc.WithAdmissionChecked(reqCtx)
	reqCtx = responseSvc.WithRawRequestBody(reqCtx, rawBody)
	reqCtx = responseSvc.WithCacheTrace(reqCtx, cacheTrace)
	if req.Stream {
		stream, err := h.responses.CreateStream(reqCtx, identity, responseReq, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, metricsSurfaceMessages, "", err)
			return
		}
		attachCacheHeaders(c, cacheTrace)
		h.streamAnthropicMessages(c, stream, req.Model, start)
		return
	}

	result, err := h.responses.Create(reqCtx, identity, responseReq, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, metricsSurfaceMessages, "", err)
		return
	}

	// upstreamLatency = total latency - (retry delays)
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(metricsSurfaceMessages, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	h.logRequestCompleted(c, metricsSurfaceMessages, result.ProviderName, http.StatusOK, time.Since(start))
	attachCacheHeaders(c, cacheTrace)
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
		if len(identity.APIKeyProviders) > 0 && !slices.Contains(identity.APIKeyProviders, p.Name()) {
			continue
		}
		if len(identity.APIKeyModels) > 0 && !slices.Contains(identity.APIKeyModels, p.Model()) {
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
				"stream":            registryBool(hasRegistry, record.SupportsStream),
				"tools":             registryBool(hasRegistry, record.SupportsTools),
				"images":            registryBool(hasRegistry, record.SupportsImages),
				"structured_output": registryBool(hasRegistry, record.SupportsStructuredOutput),
				"long_context":      hasRegistry && record.SupportsLongContext,
				"embeddings":        hasRegistry && record.SupportsEmbeddings,
			},
			"health_status":  registryString(hasRegistry, record.HealthStatus, "unknown"),
			"enabled":        registryBool(hasRegistry, record.Enabled),
			"drain":          hasRegistry && record.Drain,
			"routing_weight": registryInt(hasRegistry, record.RoutingWeight),
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": models})
}

func (h *Handler) Embeddings(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceEmbeddings)()

	var req provider.EmbeddingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.RecordError(metricsSurfaceEmbeddings, "", metricsResultClientError, "invalid_request")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	identity, ok := h.requireIdentity(c, metricsSurfaceEmbeddings)
	if !ok {
		return
	}

	providerNames, err := h.deps.Store.ListTenantProviders(c.Request.Context(), identity.TenantID)
	if err != nil {
		h.logRequestFailed(c, metricsSurfaceEmbeddings, "", http.StatusInternalServerError, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}

	var selected provider.Provider
	for _, name := range providerNames {
		p, ok := h.deps.ProviderMgr.Get(name)
		if !ok {
			continue
		}
		record, hasRegistry := h.deps.ProviderMgr.Registry(name)
		if !registryBool(hasRegistry, record.SupportsEmbeddings) {
			continue
		}
		if len(identity.APIKeyProviders) > 0 && !slices.Contains(identity.APIKeyProviders, p.Name()) {
			continue
		}
		if len(identity.APIKeyModels) > 0 && !slices.Contains(identity.APIKeyModels, p.Model()) {
			continue
		}
		selected = p
		break
	}

	if selected == nil {
		h.metrics.RecordError(metricsSurfaceEmbeddings, "", metricsResultUpstream, "no_embedding_provider")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "no embedding provider available for this request", "type": "invalid_request_error"}})
		return
	}

	result, err := selected.CreateEmbedding(c.Request.Context(), &req)
	if err != nil {
		h.renderServiceError(c, metricsSurfaceEmbeddings, selected.Name(), err)
		return
	}

	h.logRequestCompleted(c, metricsSurfaceEmbeddings, selected.Name(), http.StatusOK, time.Since(start))
	c.JSON(http.StatusOK, result)
}

func (h *Handler) ImageGenerations(c *gin.Context) {
	start := time.Now()
	defer h.metrics.TrackInFlight(metricsSurfaceImages)()

	var req provider.ImageGenerationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.RecordError(metricsSurfaceImages, "", metricsResultClientError, "invalid_request")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	identity, ok := h.requireIdentity(c, metricsSurfaceImages)
	if !ok {
		return
	}

	providerNames, err := h.deps.Store.ListTenantProviders(c.Request.Context(), identity.TenantID)
	if err != nil {
		h.logRequestFailed(c, metricsSurfaceImages, "", http.StatusInternalServerError, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error(), "type": "internal_error"}})
		return
	}

	var selected provider.Provider
	for _, name := range providerNames {
		p, ok := h.deps.ProviderMgr.Get(name)
		if !ok {
			continue
		}
		record, hasRegistry := h.deps.ProviderMgr.Registry(name)
		if !registryBool(hasRegistry, record.SupportsImages) {
			continue
		}
		if len(identity.APIKeyProviders) > 0 && !slices.Contains(identity.APIKeyProviders, p.Name()) {
			continue
		}
		model := strings.TrimSpace(req.Model)
		if model == "" {
			model = p.Model()
		}
		if len(identity.APIKeyModels) > 0 && !slices.Contains(identity.APIKeyModels, model) {
			continue
		}
		selected = p
		break
	}

	if selected == nil {
		h.metrics.RecordError(metricsSurfaceImages, "", metricsResultUpstream, "no_image_provider")
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "no image generation provider available for this request", "type": "invalid_request_error"}})
		return
	}

	result, err := selected.CreateImageGeneration(c.Request.Context(), &req)
	if err != nil {
		h.renderServiceError(c, metricsSurfaceImages, selected.Name(), err)
		return
	}

	h.logRequestCompleted(c, metricsSurfaceImages, selected.Name(), http.StatusOK, time.Since(start))
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Metrics(c *gin.Context) {
	h.metrics.Handler().ServeHTTP(c.Writer, c.Request)
}

func (h *Handler) requireIdentity(c *gin.Context, surface string) (*ports.AuthIdentity, bool) {
	identity, ok := middleware.Identity(c)
	if !ok {
		h.metrics.RecordError(surface, "", metricsResultAuthError, "invalid_api_key")
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return nil, false
	}
	return identity, true
}

func (h *Handler) Health(c *gin.Context) {
	writeOKMsg(c, "ok", gin.H{"status": "ok"})
}

func (h *Handler) Ready(c *gin.Context) {
	if len(h.deps.ProviderMgr.List()) == 0 {
		writeError(c, http.StatusServiceUnavailable, CodeNoHealthyProvider, "no providers configured")
		return
	}

	if err := h.deps.Store.Ping(c.Request.Context()); err != nil {
		slog.Error("ready check failed: db", "error", err)
		writeError(c, http.StatusServiceUnavailable, CodeServiceUnavailable, "database unavailable")
		return
	}

	if h.deps.RedisPing != nil {
		if err := h.deps.RedisPing(c.Request.Context()); err != nil {
			// Redis is fail-open: cache falls back to memory, rate limiter falls
			// back to in-memory buckets. Do not mark the pod unready.
			slog.Warn("ready check: redis unavailable, continuing fail-open", "error", err)
		}
	}

	writeOKMsg(c, "ready", gin.H{"status": "ready"})
}

func (h *Handler) observeResponse(surface, providerName string, usage provider.Usage, latency time.Duration) {
	h.metrics.RecordSuccess(surface, providerName, usage, latency, nil, 0, 0)
}

func (h *Handler) observeResponseWithUpstream(surface, providerName string, usage provider.Usage, latency, upstreamLatency time.Duration, retries, fallback int) {
	h.metrics.RecordSuccess(surface, providerName, usage, latency, &upstreamLatency, retries, fallback)
}

func isCatalogClientError(err error) bool {
	return errors.Is(err, catalog.ErrServiceNotPublished) ||
		errors.Is(err, catalog.ErrServiceDisabled) ||
		errors.Is(err, catalog.ErrServiceSurfaceDenied) ||
		errors.Is(err, catalog.ErrPromptTemplateMissing) ||
		errors.Is(err, catalog.ErrPromptVariableMissing) ||
		errors.Is(err, catalog.ErrPolicyViolation) ||
		errors.Is(err, catalog.ErrRateLimited) ||
		errors.Is(err, catalog.ErrServiceAccessDenied)
}

func (h *Handler) renderServiceError(c *gin.Context, surface, providerName string, err error) {
	wrapped := responseSvc.WrapError(err)
	httpErr := gatewayHTTPError{Status: wrapped.Status, Message: wrapped.Message, Type: wrapped.Type}
	if isCatalogClientError(err) {
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

// renderServiceErrorV2 returns errors in unified {code, success, message, data} format.
// Used by Service and Admin endpoints, NOT by LLM proxy endpoints.
func (h *Handler) renderServiceErrorV2(c *gin.Context, surface, providerName string, err error) {
	code := errToCode(err)
	httpStatus := codeToHTTPStatus(code, err)

	result, errorClass := classifyMetricsError(err, codeMessages[code])
	h.metrics.RecordError(surface, providerName, result, errorClass)
	h.logRequestFailed(c, surface, providerName, httpStatus, err)

	writeError(c, httpStatus, code, err.Error())
}

func codeToHTTPStatus(code Code, err error) int {
	switch code {
	case CodeInvalidAPIKey, CodeInactiveAPIKey, CodeInvalidSecret:
		return http.StatusUnauthorized
	case CodeInsufficientRole:
		return http.StatusForbidden
	case CodeQuotaExceeded, CodeBudgetExceeded, CodeRateLimited:
		return http.StatusTooManyRequests
	case CodeBadRequest, CodeInvalidRequestBody, CodeMissingRequiredField, CodeInvalidParameter:
		return http.StatusBadRequest
	case CodeServiceNotPublished, CodeServiceDisabled, CodeServiceAccessDenied:
		return http.StatusBadRequest
	case CodeServiceUnavailable, CodeNoHealthyProvider:
		return http.StatusServiceUnavailable
	case CodeUpstreamTimeout:
		return http.StatusGatewayTimeout
	case CodeUpstreamError:
		return http.StatusBadGateway
	default:
		// Prefer structured UpstreamError over string matching.
		var ue *provider.UpstreamError
		if errors.As(err, &ue) {
			if ue.IsTimeout() {
				return http.StatusGatewayTimeout
			}
			if ue.IsRateLimited() {
				return http.StatusTooManyRequests
			}
			if ue.StatusCode >= http.StatusBadRequest && ue.StatusCode < http.StatusInternalServerError {
				return ue.StatusCode
			}
			if ue.StatusCode >= http.StatusInternalServerError {
				return http.StatusBadGateway
			}
		}
		// Fallback: legacy string-matching for plain errors.
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "timeout") {
				return http.StatusGatewayTimeout
			}
			if strings.Contains(msg, "429") || strings.Contains(msg, "rate_limit") {
				return http.StatusTooManyRequests
			}
			if strings.Contains(msg, "400") || strings.Contains(msg, "invalid") {
				return http.StatusBadRequest
			}
		}
		return http.StatusBadGateway
	}
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
	default:
		// Prefer structured UpstreamError over string matching.
		var ue *provider.UpstreamError
		if errors.As(err, &ue) {
			if ue.IsTimeout() {
				return http.StatusGatewayTimeout
			}
			if ue.IsRateLimited() {
				return http.StatusTooManyRequests
			}
			if ue.StatusCode >= http.StatusBadRequest && ue.StatusCode < http.StatusInternalServerError {
				return ue.StatusCode
			}
			if ue.StatusCode >= http.StatusInternalServerError {
				return http.StatusBadGateway
			}
		}
		// Fallback: legacy string-matching for plain errors that don't wrap UpstreamError.
		msg := err.Error()
		if strings.Contains(msg, "timeout") {
			return http.StatusGatewayTimeout
		}
		if strings.Contains(msg, "401") || strings.Contains(msg, "authentication") {
			return http.StatusUnauthorized
		}
		if strings.Contains(msg, "403") || strings.Contains(msg, "forbidden") {
			return http.StatusForbidden
		}
		if strings.Contains(msg, "429") || strings.Contains(msg, "rate_limit") {
			return http.StatusTooManyRequests
		}
		if strings.Contains(msg, "400") || strings.Contains(msg, "invalid") {
			return http.StatusBadRequest
		}
		return http.StatusBadGateway
	}
}

// SyncCircuitBreakerStates updates metrics and persists states to Redis.
func (h *Handler) SyncCircuitBreakerStates() {
	states := h.responses.GetCircuitBreakerStates()
	for key, state := range states {
		// Parse key "tenantID:providerName"
		parts := strings.SplitN(key, ":", 2)
		if len(parts) == 2 {
			h.metrics.SetCircuitBreakerState(parts[0], parts[1], state)
		}
	}
	// Persist to Redis for cross-restart recovery.
	h.responses.PersistCircuitBreakerState(context.Background())
}

func writeSSE(c *gin.Context, payload any) error {
	return httpresponse.WriteSSE(c, payload)
}

func writeSSEEvent(c *gin.Context, eventType string, payload any) error {
	return httpresponse.WriteSSEEvent(c, eventType, payload)
}

func writeSSEDone(c *gin.Context) {
	httpresponse.WriteSSEDone(c)
}

func matchesModelFilters(c *gin.Context, record ports.ProviderRegistryRecord) bool {
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

func registryBool(hasRegistry bool, value bool) bool {
	return !hasRegistry || value
}
