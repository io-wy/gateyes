package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/gateyes/gateway/internal/config"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
)

type Metrics struct {
	enabled bool
	handler http.Handler

	llmRequests          *prometheus.CounterVec
	llmInflightRequests  *prometheus.GaugeVec
	llmRequestDuration   *prometheus.HistogramVec
	llmUpstreamDuration  *prometheus.HistogramVec
	llmTimeToFirstToken  *prometheus.HistogramVec
	llmActiveStreams     *prometheus.GaugeVec
	llmStreamDuration    *prometheus.HistogramVec
	llmTokens            *prometheus.CounterVec
	llmErrors            *prometheus.CounterVec
	llmRetries           *prometheus.CounterVec
	llmFallbacks         *prometheus.CounterVec
	providerRequests     *prometheus.CounterVec
	providerCircuitState *prometheus.GaugeVec

	providerCurrentLoad  *prometheus.GaugeVec
	providerTPM          *prometheus.GaugeVec
	providerHealthStatus *prometheus.GaugeVec
}

var registerGoCollectorOnce sync.Once

const (
	metricsSurfaceResponses       = "responses"
	metricsSurfaceChatCompletions = "chat_completions"
	metricsSurfaceMessages        = "messages"
	metricsSurfaceEmbeddings      = "embeddings"
	metricsSurfaceModels          = "models"
	metricsSurfaceAdmin           = "admin"

	metricsResultSuccess     = "success"
	metricsResultClientError = "client_error"
	metricsResultAuthError   = "auth_error"
	metricsResultRateLimited = "rate_limited"
	metricsResultTimeout     = "timeout"
	metricsResultUpstream    = "upstream_error"
	metricsResultInternal    = "internal_error"

	metricsProviderNone = "none"
)

func NewMetrics(namespace string) *Metrics {
	return NewMetricsFromConfig(config.MetricsConfig{Namespace: namespace, Enabled: true})
}

func NewMetricsFromConfig(cfg config.MetricsConfig) *Metrics {
	namespace := cfg.Namespace
	if namespace == "" {
		namespace = "gateway"
	}
	metrics := &Metrics{
		enabled: cfg.Enabled,
		handler: http.NotFoundHandler(),
	}
	if !cfg.Enabled {
		return metrics
	}

	metrics.handler = promhttp.Handler()
	registerGoCollectorOnce.Do(func() {
		if err := prometheus.Register(prometheus.NewGoCollector()); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				panic(err)
			}
		}
	})
	metrics.llmRequests = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "llm_requests_total"}, []string{"surface", "result", "provider"})
	metrics.llmInflightRequests = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "llm_inflight_requests"}, []string{"surface"})
	metrics.llmRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "llm_request_duration_seconds",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"surface", "provider", "result"})
	metrics.llmUpstreamDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "llm_upstream_duration_seconds",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"surface", "provider", "result"})
	metrics.llmTimeToFirstToken = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "llm_time_to_first_token_seconds",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"surface", "provider"})
	metrics.llmActiveStreams = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "llm_active_streams"}, []string{"surface"})
	metrics.llmStreamDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "llm_stream_duration_seconds",
		Buckets:   []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"surface", "provider", "result"})
	metrics.llmTokens = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "llm_tokens_total"}, []string{"provider", "token_type"})
	metrics.llmErrors = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "llm_errors_total"}, []string{"surface", "provider", "error_class"})
	metrics.llmRetries = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "llm_retries_total"}, []string{"provider"})
	metrics.llmFallbacks = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "llm_fallbacks_total"}, []string{"provider"})
	metrics.providerRequests = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "provider_requests_total"}, []string{"provider", "result"})
	metrics.providerCircuitState = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_circuit_state"}, []string{"tenant_id", "provider"})
	metrics.providerCurrentLoad = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_current_load"}, []string{"provider"})
	metrics.providerTPM = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_tpm"}, []string{"provider"})
	metrics.providerHealthStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_health_status"}, []string{"provider"})
	return metrics
}

func (m *Metrics) Handler() http.Handler {
	if m == nil || m.handler == nil {
		return http.NotFoundHandler()
	}
	return m.handler
}

func (m *Metrics) TrackInFlight(surface string) func() {
	if m == nil || !m.enabled {
		return func() {}
	}
	m.llmInflightRequests.WithLabelValues(surface).Inc()
	return func() {
		m.llmInflightRequests.WithLabelValues(surface).Dec()
	}
}

func (m *Metrics) TrackStream(surface string) func() {
	if m == nil || !m.enabled {
		return func() {}
	}
	m.llmActiveStreams.WithLabelValues(surface).Inc()
	return func() {
		m.llmActiveStreams.WithLabelValues(surface).Dec()
	}
}

func (m *Metrics) ObserveTTFT(surface, providerName string, latency time.Duration) {
	if m == nil || !m.enabled {
		return
	}
	m.llmTimeToFirstToken.WithLabelValues(surface, normalizeMetricsProvider(providerName)).Observe(latency.Seconds())
}

func (m *Metrics) ObserveStreamDuration(surface, providerName, result string, duration time.Duration) {
	if m == nil || !m.enabled {
		return
	}
	m.llmStreamDuration.WithLabelValues(surface, normalizeMetricsProvider(providerName), result).Observe(duration.Seconds())
}

func (m *Metrics) RecordSuccess(surface, providerName string, usage provider.Usage, latency time.Duration, upstreamLatency *time.Duration, retries, fallback int) {
	if m == nil || !m.enabled {
		return
	}
	providerLabel := normalizeMetricsProvider(providerName)
	m.recordTokens(providerLabel, usage)
	m.llmRequests.WithLabelValues(surface, metricsResultSuccess, providerLabel).Inc()
	m.providerRequests.WithLabelValues(providerLabel, metricsResultSuccess).Inc()
	m.llmRequestDuration.WithLabelValues(surface, providerLabel, metricsResultSuccess).Observe(latency.Seconds())
	if upstreamLatency != nil {
		m.llmUpstreamDuration.WithLabelValues(surface, providerLabel, metricsResultSuccess).Observe(upstreamLatency.Seconds())
	}
	if retries > 0 {
		m.llmRetries.WithLabelValues(providerLabel).Add(float64(retries))
	}
	if fallback > 0 {
		m.llmFallbacks.WithLabelValues(providerLabel).Add(float64(fallback))
	}
}

func (m *Metrics) RecordError(surface, providerName, result, errorClass string) {
	if m == nil || !m.enabled {
		return
	}
	providerLabel := normalizeMetricsProvider(providerName)
	m.llmRequests.WithLabelValues(surface, result, providerLabel).Inc()
	m.llmErrors.WithLabelValues(surface, providerLabel, errorClass).Inc()
	m.providerRequests.WithLabelValues(providerLabel, result).Inc()
}

func (m *Metrics) SetCircuitBreakerState(tenantID, providerName string, state int) {
	if m == nil || !m.enabled {
		return
	}
	m.providerCircuitState.WithLabelValues(tenantID, normalizeMetricsProvider(providerName)).Set(float64(state))
}

func (m *Metrics) recordTokens(providerName string, usage provider.Usage) {
	m.llmTokens.WithLabelValues(providerName, "prompt").Add(float64(usage.PromptTokens))
	m.llmTokens.WithLabelValues(providerName, "completion").Add(float64(usage.CompletionTokens))
	m.llmTokens.WithLabelValues(providerName, "total").Add(float64(usage.TotalTokens))
	if usage.CachedTokens > 0 {
		m.llmTokens.WithLabelValues(providerName, "cached").Add(float64(usage.CachedTokens))
	}
}

func normalizeMetricsProvider(providerName string) string {
	if providerName == "" {
		return metricsProviderNone
	}
	return providerName
}

func NormalizeMetricsProvider(providerName string) string {
	return normalizeMetricsProvider(providerName)
}

func (m *Metrics) StartProviderStatsExporter(ctx context.Context, stats *provider.Stats, interval time.Duration) {
	if m == nil || !m.enabled || stats == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.exportProviderStats(stats)
		case <-ctx.Done():
			return
		}
	}
}

func (m *Metrics) exportProviderStats(stats *provider.Stats) {
	for _, item := range stats.List() {
		providerLabel := normalizeMetricsProvider(item.Name)
		m.providerCurrentLoad.WithLabelValues(providerLabel).Set(float64(item.CurrentLoad))
		m.providerTPM.WithLabelValues(providerLabel).Set(float64(stats.TPM(item.Name)))
		statusValue := providerHealthStatusValue(item.Status)
		m.providerHealthStatus.WithLabelValues(providerLabel).Set(float64(statusValue))
	}
}

func providerHealthStatusValue(status string) int {
	switch status {
	case "healthy":
		return 0
	case "degraded":
		return 1
	case "unhealthy":
		return 2
	default:
		return 3
	}
}

func classifyMetricsError(err error, httpErrType string) (result, errorClass string) {
	var upstreamErr *provider.UpstreamError
	if errors.As(err, &upstreamErr) {
		switch {
		case upstreamErr.IsTimeout():
			return metricsResultTimeout, "timeout"
		case upstreamErr.IsRateLimited():
			return metricsResultRateLimited, "upstream_rate_limited"
		case upstreamErr.StatusCode >= 500:
			return metricsResultUpstream, "upstream_5xx"
		case upstreamErr.StatusCode >= 400:
			return metricsResultUpstream, "upstream_4xx"
		}
	}

	errMsg := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, responseSvc.ErrNoProvider):
		return metricsResultInternal, "no_provider"
	case strings.Contains(errMsg, "invalid api key"):
		return metricsResultAuthError, "invalid_api_key"
	case strings.Contains(errMsg, "inactive api key"):
		return metricsResultAuthError, "inactive_api_key"
	case strings.Contains(errMsg, "forbidden"):
		return metricsResultAuthError, "forbidden"
	case strings.Contains(errMsg, "quota exceeded"):
		return metricsResultRateLimited, "quota_exceeded"
	case strings.Contains(errMsg, "rate_limit") || strings.Contains(errMsg, "429"):
		return metricsResultRateLimited, "rate_limited"
	case strings.Contains(errMsg, "timeout"):
		return metricsResultTimeout, "timeout"
	case httpErrType == "invalid_request_error":
		return metricsResultClientError, "invalid_request"
	default:
		return metricsResultInternal, "internal_error"
	}
}
