package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

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

type Metrics struct {
	// 请求计数
	requests         *prometheus.CounterVec
	inflightRequests prometheus.Gauge

	// 延迟
	latency          *prometheus.HistogramVec
	upstreamLatency  *prometheus.HistogramVec
	timeToFirstToken *prometheus.HistogramVec

	// Tokens
	promptTokens     *prometheus.CounterVec
	completionTokens *prometheus.CounterVec
	totalTokens      *prometheus.CounterVec

	// 错误分类
	errors           *prometheus.CounterVec
	upstreamErrors   *prometheus.CounterVec
	timeouts         *prometheus.CounterVec
	rateLimited      *prometheus.CounterVec
	tokenRateLimited *prometheus.CounterVec

	// Streaming
	activeStreams  prometheus.Gauge
	streamDuration *prometheus.HistogramVec

	// Model 维度
	modelRequests *prometheus.CounterVec
	modelFailures *prometheus.CounterVec
	modelFallback *prometheus.CounterVec

	// Retry/Fallback
	retries  *prometheus.CounterVec
	fallback *prometheus.CounterVec

	// Circuit Breaker
	circuitBreakerState *prometheus.GaugeVec
}

func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		// 请求计数
		requests:         promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "requests_total"}, []string{"model", "status"}),
		inflightRequests: promauto.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Name: "inflight_requests"}),

		// 延迟
		latency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_latency_seconds",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"model"}),
		upstreamLatency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "upstream_latency_seconds",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"model"}),
		timeToFirstToken: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "time_to_first_token_seconds",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"model"}),

		// Tokens
		promptTokens:     promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "prompt_tokens_total"}, []string{"model"}),
		completionTokens: promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "completion_tokens_total"}, []string{"model"}),
		totalTokens:      promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "total_tokens_total"}, []string{"model"}),

		// 错误分类
		errors:           promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "errors_total"}, []string{"model", "type"}),
		upstreamErrors:   promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "upstream_errors_total"}, []string{"model"}),
		timeouts:         promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "timeouts_total"}, []string{"model"}),
		rateLimited:      promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "rate_limited_total"}, []string{"model"}),
		tokenRateLimited: promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "token_rate_limited_total"}, []string{"model"}),

		// Streaming
		activeStreams: promauto.NewGauge(prometheus.GaugeOpts{Namespace: namespace, Name: "active_streams"}),
		streamDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "stream_duration_seconds",
			Buckets:   []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"model"}),

		// Model 维度
		modelRequests: promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "model_requests_total"}, []string{"model"}),
		modelFailures: promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "model_failures_total"}, []string{"model"}),
		modelFallback: promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "model_fallback_total"}, []string{"model"}),

		// Retry/Fallback
		retries:  promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "retries_total"}, []string{"model"}),
		fallback: promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "fallback_total"}, []string{"model"}),

		// Circuit Breaker
		circuitBreakerState: promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "circuit_breaker_state"}, []string{"tenant_id", "provider"}),
	}
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
	h.metrics.inflightRequests.Inc()
	defer h.metrics.inflightRequests.Dec()

	var req apicompat.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.errors.WithLabelValues(req.Model, "invalid_request").Inc()
		h.metrics.modelFailures.WithLabelValues(req.Model).Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	identity, ok := middleware.Identity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	responseReq := apicompat.ConvertChatRequest(&req)
	if req.Stream {
		stream, err := h.responses.CreateStream(c.Request.Context(), identity, responseReq, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, req.Model, err)
			return
		}
		h.streamChatCompatibility(c, stream, req.Model, start)
		return
	}

	result, err := h.responses.Create(c.Request.Context(), identity, responseReq, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, req.Model, err)
		return
	}

	// upstreamLatency = total latency - (retry delays)
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(req.Model, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
	c.JSON(http.StatusOK, apicompat.ConvertResponseToChat(result.Response))
}

func (h *Handler) Responses(c *gin.Context) {
	h.handleResponsesCreate(c)
}

func (h *Handler) AnthropicMessages(c *gin.Context) {
	start := time.Now()
	h.metrics.inflightRequests.Inc()
	defer h.metrics.inflightRequests.Dec()

	var req apicompat.AnthropicMessagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.errors.WithLabelValues(req.Model, "invalid_request").Inc()
		h.metrics.modelFailures.WithLabelValues(req.Model).Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	identity, ok := middleware.Identity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	responseReq := apicompat.ConvertAnthropicRequest(&req)
	if req.Stream {
		stream, err := h.responses.CreateStream(c.Request.Context(), identity, responseReq, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, req.Model, err)
			return
		}
		h.streamAnthropicMessages(c, stream, req.Model, start)
		return
	}

	result, err := h.responses.Create(c.Request.Context(), identity, responseReq, c.GetHeader("X-Session-ID"))
	if err != nil {
		h.renderServiceError(c, req.Model, err)
		return
	}

	// upstreamLatency = total latency - (retry delays)
	upstreamLatency := time.Duration(result.LatencyMs) * time.Millisecond
	h.observeResponseWithUpstream(req.Model, result.ProviderName, result.Response.Usage, time.Since(start), upstreamLatency, result.Retries, result.Fallback)
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
	promhttp.Handler().ServeHTTP(c.Writer, c.Request)
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

func (h *Handler) observeResponse(requestedModel, providerName string, usage provider.Usage, latency time.Duration) {
	modelLabel := providerName
	if modelLabel == "" {
		modelLabel = requestedModel
	}

	// Token 计数
	h.metrics.promptTokens.WithLabelValues(modelLabel).Add(float64(usage.PromptTokens))
	h.metrics.completionTokens.WithLabelValues(modelLabel).Add(float64(usage.CompletionTokens))
	h.metrics.totalTokens.WithLabelValues(modelLabel).Add(float64(usage.TotalTokens))

	// 请求计数
	h.metrics.requests.WithLabelValues(modelLabel, "success").Inc()
	h.metrics.modelRequests.WithLabelValues(modelLabel).Inc()

	// 延迟
	h.metrics.latency.WithLabelValues(modelLabel).Observe(latency.Seconds())
	// upstreamLatency 和 timeToFirstToken 由流式处理单独记录
}

func (h *Handler) observeResponseWithUpstream(requestedModel, providerName string, usage provider.Usage, latency, upstreamLatency time.Duration, retries, fallback int) {
	modelLabel := providerName
	if modelLabel == "" {
		modelLabel = requestedModel
	}
	status := "success"

	// Token 计数
	h.metrics.promptTokens.WithLabelValues(modelLabel).Add(float64(usage.PromptTokens))
	h.metrics.completionTokens.WithLabelValues(modelLabel).Add(float64(usage.CompletionTokens))
	h.metrics.totalTokens.WithLabelValues(modelLabel).Add(float64(usage.TotalTokens))

	// 请求计数
	h.metrics.requests.WithLabelValues(modelLabel, status).Inc()
	h.metrics.modelRequests.WithLabelValues(modelLabel).Inc()

	// 延迟
	h.metrics.latency.WithLabelValues(modelLabel).Observe(latency.Seconds())
	h.metrics.upstreamLatency.WithLabelValues(modelLabel).Observe(upstreamLatency.Seconds())

	// Retry/Fallback 计数
	if retries > 0 {
		h.metrics.retries.WithLabelValues(modelLabel).Add(float64(retries))
	}
	if fallback > 0 {
		h.metrics.fallback.WithLabelValues(modelLabel).Add(float64(fallback))
		h.metrics.modelFallback.WithLabelValues(modelLabel).Add(float64(fallback))
	}
}

func (h *Handler) renderServiceError(c *gin.Context, model string, err error) {
	httpErr := responseSvc.WrapError(err)
	status := httpErr.Status
	if status >= http.StatusInternalServerError {
		status = h.inferHTTPStatus(err)
	}

	// 分类错误
	h.metrics.errors.WithLabelValues(model, httpErr.Type).Inc()
	h.metrics.modelFailures.WithLabelValues(model).Inc()

	// 检查是否是上游错误、超时或限流
	var upstreamErr *provider.UpstreamError
	if errors.As(err, &upstreamErr) {
		if upstreamErr.IsUpstream() {
			h.metrics.upstreamErrors.WithLabelValues(model).Inc()
		}
		if upstreamErr.IsTimeout() {
			h.metrics.timeouts.WithLabelValues(model).Inc()
		}
		if upstreamErr.IsRateLimited() {
			h.metrics.rateLimited.WithLabelValues(model).Inc()
		}
	} else {
		// 回退到字符串匹配
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "timeout") {
			h.metrics.timeouts.WithLabelValues(model).Inc()
		}
		if strings.Contains(errMsg, "rate_limit") || strings.Contains(errMsg, "429") {
			h.metrics.rateLimited.WithLabelValues(model).Inc()
		}
		if strings.Contains(errMsg, "5") || strings.Contains(errMsg, "upstream") {
			h.metrics.upstreamErrors.WithLabelValues(model).Inc()
		}
	}

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
			h.metrics.circuitBreakerState.WithLabelValues(parts[0], parts[1]).Set(float64(state))
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
