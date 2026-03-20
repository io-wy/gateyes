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

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/middleware"
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
	requests  *prometheus.CounterVec
	latency   *prometheus.HistogramVec
	tokens    *prometheus.CounterVec
	errors    *prometheus.CounterVec
	cacheHit  prometheus.Counter
	cacheMiss prometheus.Counter
}

func NewMetrics(namespace string) *Metrics {
	return &Metrics{
		requests: promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "requests_total"}, []string{"model", "status"}),
		latency: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_seconds",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 10},
		}, []string{"model"}),
		tokens:    promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "tokens_total"}, []string{"model", "type"}),
		errors:    promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "errors_total"}, []string{"model", "type"}),
		cacheHit:  promauto.NewCounter(prometheus.CounterOpts{Namespace: namespace, Name: "cache_hit_total"}),
		cacheMiss: promauto.NewCounter(prometheus.CounterOpts{Namespace: namespace, Name: "cache_miss_total"}),
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

	var req provider.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.metrics.errors.WithLabelValues(req.Model, "invalid_request").Inc()
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
		return
	}

	identity, ok := middleware.Identity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"message": "invalid API key", "type": "invalid_request_error"}})
		return
	}

	responseReq := provider.ConvertChatRequest(&req)
	if req.Stream {
		stream, err := h.responses.CreateStream(c.Request.Context(), identity, responseReq, c.GetHeader("X-Session-ID"))
		if err != nil {
			h.renderServiceError(c, req.Model, err)
			return
		}
		h.streamChatCompatibility(c, stream, req.Model)
		return
	}

	result, err := h.responses.Create(c.Request.Context(), identity, responseReq, c.GetHeader("X-Session-ID"))
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
	c.JSON(http.StatusOK, provider.ConvertResponseToChat(result.Response))
}

func (h *Handler) Responses(c *gin.Context) {
	h.handleResponsesCreate(c)
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
	http.DefaultServeMux.ServeHTTP(c.Writer, c.Request)
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

func (h *Handler) observeResponse(requestedModel, providerName string, usage provider.Usage, latency time.Duration, cacheHit bool) {
	modelLabel := providerName
	if modelLabel == "" || cacheHit {
		modelLabel = requestedModel
	}
	status := "success"
	if cacheHit {
		status = "cache_hit"
	}

	h.metrics.tokens.WithLabelValues(modelLabel, "prompt").Add(float64(usage.PromptTokens))
	h.metrics.tokens.WithLabelValues(modelLabel, "completion").Add(float64(usage.CompletionTokens))
	h.metrics.requests.WithLabelValues(modelLabel, status).Inc()
	h.metrics.latency.WithLabelValues(modelLabel).Observe(latency.Seconds())
}

func (h *Handler) renderServiceError(c *gin.Context, model string, err error) {
	httpErr := responseSvc.WrapError(err)
	status := httpErr.Status
	if status >= http.StatusInternalServerError {
		status = h.inferHTTPStatus(err)
	}
	h.metrics.errors.WithLabelValues(model, httpErr.Type).Inc()
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
