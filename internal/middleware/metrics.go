package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	Registry        *prometheus.Registry
	requestCount    *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
}

func NewMetrics(namespace string) *Metrics {
	if namespace == "" {
		namespace = "gateyes"
	}

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

	registry.MustRegister(requestCount, requestDuration)

	return &Metrics{
		Registry:        registry,
		requestCount:    requestCount,
		requestDuration: requestDuration,
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
			m.requestDuration.WithLabelValues(r.Method, path, status).Observe(time.Since(start).Seconds())
		})
	}
}

func normalizePath(path string) string {
	switch {
	case strings.HasPrefix(path, "/v1/"):
		return "/v1/*"
	case strings.HasPrefix(path, "/prod/"):
		return "/prod/*"
	case strings.HasPrefix(path, "/mcp/"):
		return "/mcp/*"
	default:
		return path
	}
}
