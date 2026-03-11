package middleware

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"gateyes/internal/config"
	"gateyes/internal/requestmeta"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	Registry         *prometheus.Registry
	requestCount     *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	gatewayRequests  *prometheus.CounterVec
	gatewayDuration  *prometheus.HistogramVec
	tpmEstimated     *prometheus.CounterVec
	tpmActual        *prometheus.CounterVec
	retryTotal       *prometheus.CounterVec
	fallbackTotal    *prometheus.CounterVec
	circuitOpenTotal *prometheus.CounterVec
	cacheHitTotal    *prometheus.CounterVec
	cacheMissTotal   *prometheus.CounterVec

	options          MetricsOptions
	virtualKeyLabels *labelLimiter
	modelLabels      *labelLimiter
	providerLabels   *labelLimiter
}

type MetricsOptions struct {
	MaxVirtualKeyLabels int
	MaxModelLabels      int
	MaxProviderLabels   int
	MaskVirtualKey      bool
	LabelValueMaxLen    int
}

type labelLimiter struct {
	max  int
	mu   sync.Mutex
	seen map[string]struct{}
}

func NewMetricsFromConfig(cfg config.MetricsConfig) *Metrics {
	return NewMetricsWithOptions(cfg.Namespace, MetricsOptions{
		MaxVirtualKeyLabels: cfg.MaxVirtualKeyLabels,
		MaxModelLabels:      cfg.MaxModelLabels,
		MaxProviderLabels:   cfg.MaxProviderLabels,
		MaskVirtualKey:      cfg.MaskVirtualKey,
		LabelValueMaxLen:    cfg.LabelValueMaxLen,
	})
}

func NewMetrics(namespace string) *Metrics {
	return NewMetricsWithOptions(namespace, MetricsOptions{
		MaskVirtualKey: true,
	})
}

func NewMetricsWithOptions(namespace string, options MetricsOptions) *Metrics {
	if namespace == "" {
		namespace = "gateyes"
	}
	options = normalizeMetricsOptions(options)

	registry := prometheus.NewRegistry()
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	requestCount := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "http_requests_total",
		Help:      "Total number of HTTP requests.",
	}, []string{"method", "path", "status"})

	requestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request duration in seconds.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "path", "status"})

	gatewayRequests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "gateway_requests_total",
		Help:      "Gateway requests by virtual key/model/provider/status.",
	}, []string{"virtual_key", "model", "provider", "status"})

	gatewayDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "gateway_request_duration_seconds",
		Help:      "Gateway request duration by virtual key/model/provider/status.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"virtual_key", "model", "provider", "status"})

	tpmEstimated := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "gateway_tpm_estimated_total",
		Help:      "Estimated tokens consumed.",
	}, []string{"virtual_key", "model", "provider"})

	tpmActual := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "gateway_tpm_actual_total",
		Help:      "Actual tokens consumed from provider usage fields.",
	}, []string{"virtual_key", "model", "provider"})

	retryTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "gateway_retry_total",
		Help:      "Total retry attempts.",
	}, []string{"virtual_key", "model", "provider"})

	fallbackTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "gateway_fallback_total",
		Help:      "Total provider fallback events.",
	}, []string{"virtual_key", "model", "provider"})

	circuitOpenTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "gateway_circuit_open_total",
		Help:      "Total circuit open events.",
	}, []string{"virtual_key", "provider"})

	cacheHitTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "gateway_cache_hit_total",
		Help:      "Total cache hits.",
	}, []string{"virtual_key", "model", "provider"})

	cacheMissTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "gateway_cache_miss_total",
		Help:      "Total cache misses.",
	}, []string{"virtual_key", "model", "provider"})

	registry.MustRegister(
		requestCount,
		requestDuration,
		gatewayRequests,
		gatewayDuration,
		tpmEstimated,
		tpmActual,
		retryTotal,
		fallbackTotal,
		circuitOpenTotal,
		cacheHitTotal,
		cacheMissTotal,
	)

	return &Metrics{
		Registry:         registry,
		requestCount:     requestCount,
		requestDuration:  requestDuration,
		gatewayRequests:  gatewayRequests,
		gatewayDuration:  gatewayDuration,
		tpmEstimated:     tpmEstimated,
		tpmActual:        tpmActual,
		retryTotal:       retryTotal,
		fallbackTotal:    fallbackTotal,
		circuitOpenTotal: circuitOpenTotal,
		cacheHitTotal:    cacheHitTotal,
		cacheMissTotal:   cacheMissTotal,
		options:          options,
		virtualKeyLabels: newLabelLimiter(options.MaxVirtualKeyLabels),
		modelLabels:      newLabelLimiter(options.MaxModelLabels),
		providerLabels:   newLabelLimiter(options.MaxProviderLabels),
	}
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

func (m *Metrics) Middleware(enabled bool) Middleware {
	if !enabled {
		return Noop()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			recorder := newResponseRecorder(w)
			start := time.Now()
			next.ServeHTTP(recorder, r)

			path := normalizePath(r.URL.Path)
			status := http.StatusText(recorder.status)
			if status == "" {
				status = "unknown"
			}

			m.requestCount.WithLabelValues(r.Method, path, status).Inc()
			duration := time.Since(start).Seconds()
			m.requestDuration.WithLabelValues(r.Method, path, status).Observe(duration)

			virtualKey := m.normalizeVirtualKeyLabel(r.Header.Get(requestmeta.HeaderVirtualKey))
			model := m.normalizeModelLabel(r.Header.Get(requestmeta.HeaderResolvedModel))
			provider := m.normalizeProviderLabel(r.Header.Get(requestmeta.HeaderResolvedProvider))
			statusCode := strconv.Itoa(recorder.status)
			m.gatewayRequests.WithLabelValues(virtualKey, model, provider, statusCode).Inc()
			m.gatewayDuration.WithLabelValues(virtualKey, model, provider, statusCode).Observe(duration)

			estimated := parseInt64Header(r.Header.Get(requestmeta.HeaderUsageEstimatedTokens))
			if estimated > 0 {
				m.tpmEstimated.WithLabelValues(virtualKey, model, provider).Add(float64(estimated))
			}
			actual := parseInt64Header(r.Header.Get(requestmeta.HeaderUsageTotalTokens))
			if actual > 0 {
				m.tpmActual.WithLabelValues(virtualKey, model, provider).Add(float64(actual))
			}

			retryCount := parseInt64Header(r.Header.Get(requestmeta.HeaderRetryCount))
			if retryCount > 0 {
				m.retryTotal.WithLabelValues(virtualKey, model, provider).Add(float64(retryCount))
			}
			fallbackCount := parseInt64Header(r.Header.Get(requestmeta.HeaderFallbackCount))
			if fallbackCount > 0 {
				m.fallbackTotal.WithLabelValues(virtualKey, model, provider).Add(float64(fallbackCount))
			}
			circuitOpenCount := parseInt64Header(r.Header.Get(requestmeta.HeaderCircuitOpenCount))
			if circuitOpenCount > 0 {
				m.circuitOpenTotal.WithLabelValues(virtualKey, provider).Add(float64(circuitOpenCount))
			}

			cacheStatus := normalizeMetricLabel(r.Header.Get(requestmeta.HeaderCacheStatus), m.options.LabelValueMaxLen, "")
			if cacheStatus == "" {
				cacheStatus = normalizeMetricLabel(recorder.Header().Get("X-Cache"), m.options.LabelValueMaxLen, "")
			}
			switch strings.ToUpper(cacheStatus) {
			case "HIT":
				m.cacheHitTotal.WithLabelValues(virtualKey, model, provider).Inc()
			case "MISS":
				m.cacheMissTotal.WithLabelValues(virtualKey, model, provider).Inc()
			}
		})
	}
}

func normalizeMetricLabel(value string, maxLen int, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	if maxLen > 0 && len(trimmed) > maxLen {
		return trimmed[:maxLen]
	}
	return trimmed
}

func normalizeMetricsOptions(options MetricsOptions) MetricsOptions {
	if options.MaxVirtualKeyLabels <= 0 {
		options.MaxVirtualKeyLabels = 200
	}
	if options.MaxModelLabels <= 0 {
		options.MaxModelLabels = 200
	}
	if options.MaxProviderLabels <= 0 {
		options.MaxProviderLabels = 64
	}
	if options.LabelValueMaxLen <= 0 {
		options.LabelValueMaxLen = 64
	}
	return options
}

func newLabelLimiter(max int) *labelLimiter {
	return &labelLimiter{
		max:  max,
		seen: make(map[string]struct{}, max),
	}
}

func parseInt64Header(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func (l *labelLimiter) normalize(value string) string {
	if l == nil {
		return value
	}
	if l.max <= 0 {
		return value
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, ok := l.seen[value]; ok {
		return value
	}
	if len(l.seen) < l.max {
		l.seen[value] = struct{}{}
		return value
	}
	return "other"
}

func (m *Metrics) normalizeVirtualKeyLabel(raw string) string {
	label := normalizeMetricLabel(raw, m.options.LabelValueMaxLen, "unknown")
	if label == "unknown" {
		return label
	}
	if m.options.MaskVirtualKey {
		sum := sha1.Sum([]byte(label))
		label = "vk_" + hex.EncodeToString(sum[:6])
	}
	return m.virtualKeyLabels.normalize(label)
}

func (m *Metrics) normalizeModelLabel(raw string) string {
	label := normalizeMetricLabel(raw, m.options.LabelValueMaxLen, "unknown")
	return m.modelLabels.normalize(label)
}

func (m *Metrics) normalizeProviderLabel(raw string) string {
	label := normalizeMetricLabel(raw, m.options.LabelValueMaxLen, "unknown")
	return m.providerLabels.normalize(label)
}

func normalizePath(path string) string {
	switch {
	case strings.HasPrefix(path, "/v1/"):
		return "/v1/*"
	case strings.HasPrefix(path, "/prod/"):
		return "/prod/*"
	default:
		return path
	}
}
