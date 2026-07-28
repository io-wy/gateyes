package handler

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/service/provider"
	responseSvc "github.com/gateyes/gateway/internal/service/responses"
	"github.com/gateyes/gateway/internal/service/router"
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
	llmPromptCacheRatio  *prometheus.HistogramVec
	llmErrors            *prometheus.CounterVec
	llmRetries           *prometheus.CounterVec
	llmFallbacks         *prometheus.CounterVec
	providerRequests     *prometheus.CounterVec
	providerCircuitState *prometheus.GaugeVec

	providerCurrentLoad   *prometheus.GaugeVec
	providerTPM           *prometheus.GaugeVec
	providerHealthStatus  *prometheus.GaugeVec
	providerGPUCacheUsage *prometheus.GaugeVec
	providerCPUCacheUsage *prometheus.GaugeVec
	providerCacheHitRate  *prometheus.GaugeVec
	providerCacheQueries  *prometheus.GaugeVec
	providerCacheHits     *prometheus.GaugeVec

	cacheLookups     *prometheus.CounterVec
	cacheWrites      *prometheus.CounterVec
	cacheValueSize   *prometheus.HistogramVec
	cacheGetDuration *prometheus.HistogramVec
	cacheStats       cacheStats

	eventBusDropped   prometheus.Counter
	eventBusProcessed prometheus.Counter
	eventBusPanics    prometheus.Counter
	eventBusQueueSize prometheus.Gauge

	inferenceScraper *router.InferenceScraper
	scraperMu        sync.RWMutex
}

type CacheLookupSummary struct {
	Hit   int64 `json:"hit"`
	Miss  int64 `json:"miss"`
	Error int64 `json:"error"`
	Skip  int64 `json:"skip"`
	Total int64 `json:"total"`
}

type CacheWriteSummary struct {
	Success int64 `json:"success"`
	Error   int64 `json:"error"`
	Total   int64 `json:"total"`
}

type CacheLayerSummary struct {
	Layer         string             `json:"layer"`
	Lookups       CacheLookupSummary `json:"lookups"`
	Writes        CacheWriteSummary  `json:"writes"`
	HitRate       float64            `json:"hit_rate"`
	LookupAvgMs   float64            `json:"lookup_avg_ms"`
	ValueAvgBytes float64            `json:"value_avg_bytes"`
}

type CacheSummary struct {
	Enabled bool                `json:"enabled"`
	Layers  []CacheLayerSummary `json:"layers"`
	Totals  CacheLayerSummary   `json:"totals"`
}

type cacheStats struct {
	mu             sync.RWMutex
	lookups        map[string]map[string]int64
	writes         map[string]map[string]int64
	lookupDuration map[string]cacheDurationStats
	valueSize      map[string]cacheValueStats
}

type cacheDurationStats struct {
	count int64
	total time.Duration
}

type cacheValueStats struct {
	count int64
	total int64
}

func newCacheStats() cacheStats {
	return cacheStats{
		lookups:        make(map[string]map[string]int64),
		writes:         make(map[string]map[string]int64),
		lookupDuration: make(map[string]cacheDurationStats),
		valueSize:      make(map[string]cacheValueStats),
	}
}

var registerGoCollectorOnce sync.Once

const (
	metricsSurfaceResponses       = "responses"
	metricsSurfaceChatCompletions = "chat_completions"
	metricsSurfaceMessages        = "messages"
	metricsSurfaceEmbeddings      = "embeddings"
	metricsSurfaceImages          = "images"
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
		enabled:    cfg.Enabled,
		handler:    http.NotFoundHandler(),
		cacheStats: newCacheStats(),
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
	metrics.llmPromptCacheRatio = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "llm_prompt_cache_ratio",
		Help:      "Ratio of cached prompt tokens to total prompt tokens per request, reported by the upstream provider.",
		Buckets:   []float64{0, 0.1, 0.25, 0.5, 0.75, 0.9, 0.99, 1},
	}, []string{"provider"})
	metrics.llmErrors = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "llm_errors_total"}, []string{"surface", "provider", "error_class"})
	metrics.llmRetries = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "llm_retries_total"}, []string{"provider"})
	metrics.llmFallbacks = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "llm_fallbacks_total"}, []string{"provider"})
	metrics.providerRequests = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "provider_requests_total"}, []string{"provider", "result"})
	metrics.providerCircuitState = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_circuit_state"}, []string{"tenant_id", "provider"})
	metrics.providerCurrentLoad = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_current_load"}, []string{"provider"})
	metrics.providerTPM = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_tpm"}, []string{"provider"})
	metrics.providerHealthStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_health_status"}, []string{"provider"})
	metrics.providerGPUCacheUsage = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_gpu_cache_usage_ratio", Help: "vLLM GPU KV-cache fill ratio scraped from the provider's /metrics endpoint."}, []string{"provider"})
	metrics.providerCPUCacheUsage = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_cpu_cache_usage_ratio", Help: "vLLM CPU KV-cache fill ratio scraped from the provider's /metrics endpoint."}, []string{"provider"})
	metrics.providerCacheHitRate = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_prefix_cache_hit_rate_ratio", Help: "vLLM prefix-cache hit rate scraped from the provider's /metrics endpoint."}, []string{"provider"})
	metrics.providerCacheQueries = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_prefix_cache_queries", Help: "vLLM prefix-cache total queries scraped from the provider's /metrics endpoint."}, []string{"provider"})
	metrics.providerCacheHits = promauto.NewGaugeVec(prometheus.GaugeOpts{Namespace: namespace, Name: "provider_prefix_cache_hits", Help: "vLLM prefix-cache hits scraped from the provider's /metrics endpoint."}, []string{"provider"})
	metrics.cacheLookups = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "cache_lookups_total"}, []string{"layer", "result"})
	metrics.cacheWrites = promauto.NewCounterVec(prometheus.CounterOpts{Namespace: namespace, Name: "cache_writes_total"}, []string{"layer", "result"})
	metrics.cacheValueSize = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "cache_value_size_bytes",
		Buckets:   []float64{128, 512, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304},
	}, []string{"layer"})
	metrics.cacheGetDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "cache_get_duration_seconds",
		Buckets:   []float64{0.0001, 0.001, 0.01, 0.1, 1},
	}, []string{"layer"})
	metrics.eventBusDropped = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "eventbus_dropped_total",
		Help:      "Number of event bus handlers dropped because the buffer was full or the bus was closed.",
	})
	metrics.eventBusProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "eventbus_processed_total",
		Help:      "Number of event bus handlers that were invoked (including those that panicked).",
	})
	metrics.eventBusPanics = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "eventbus_panics_total",
		Help:      "Number of event bus handlers that panicked during execution.",
	})
	metrics.eventBusQueueSize = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "eventbus_queue_size",
		Help:      "Current number of handlers waiting in the event bus buffer.",
	})
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
	if usage.PromptTokens > 0 {
		ratio := float64(usage.CachedTokens) / float64(usage.PromptTokens)
		m.llmPromptCacheRatio.WithLabelValues(providerName).Observe(ratio)
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

func (m *Metrics) RecordCacheLookup(layer, result string) {
	if m == nil || !m.enabled {
		return
	}
	m.cacheLookups.WithLabelValues(layer, result).Inc()
	m.cacheStats.recordLookup(layer, result)
}

func (m *Metrics) RecordCacheWrite(layer, result string) {
	if m == nil || !m.enabled {
		return
	}
	m.cacheWrites.WithLabelValues(layer, result).Inc()
	m.cacheStats.recordWrite(layer, result)
}

func (m *Metrics) ObserveCacheValueSize(layer string, size int) {
	if m == nil || !m.enabled {
		return
	}
	m.cacheValueSize.WithLabelValues(layer).Observe(float64(size))
	m.cacheStats.observeValueSize(layer, size)
}

func (m *Metrics) ObserveCacheGetDuration(layer string, d time.Duration) {
	if m == nil || !m.enabled {
		return
	}
	m.cacheGetDuration.WithLabelValues(layer).Observe(d.Seconds())
	m.cacheStats.observeLookupDuration(layer, d)
}

func (m *Metrics) CacheSummary() CacheSummary {
	if m == nil {
		return CacheSummary{}
	}
	return m.cacheStats.summary(m.enabled)
}

func (s *cacheStats) recordLookup(layer, result string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lookups[layer] == nil {
		s.lookups[layer] = make(map[string]int64)
	}
	s.lookups[layer][result]++
}

func (s *cacheStats) recordWrite(layer, result string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writes[layer] == nil {
		s.writes[layer] = make(map[string]int64)
	}
	s.writes[layer][result]++
}

func (s *cacheStats) observeValueSize(layer string, size int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.valueSize[layer]
	stats.count++
	stats.total += int64(size)
	s.valueSize[layer] = stats
}

func (s *cacheStats) observeLookupDuration(layer string, d time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.lookupDuration[layer]
	stats.count++
	stats.total += d
	s.lookupDuration[layer] = stats
}

func (s *cacheStats) summary(enabled bool) CacheSummary {
	if s == nil {
		return CacheSummary{Enabled: enabled}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	layerSet := make(map[string]struct{})
	for layer := range s.lookups {
		layerSet[layer] = struct{}{}
	}
	for layer := range s.writes {
		layerSet[layer] = struct{}{}
	}
	for layer := range s.lookupDuration {
		layerSet[layer] = struct{}{}
	}
	for layer := range s.valueSize {
		layerSet[layer] = struct{}{}
	}

	layers := make([]string, 0, len(layerSet))
	for layer := range layerSet {
		layers = append(layers, layer)
	}
	sort.Strings(layers)

	summary := CacheSummary{
		Enabled: enabled,
		Layers:  make([]CacheLayerSummary, 0, len(layers)),
		Totals:  CacheLayerSummary{Layer: "total"},
	}
	for _, layer := range layers {
		layerSummary := s.layerSummaryLocked(layer)
		summary.Layers = append(summary.Layers, layerSummary)
		summary.Totals.Lookups.Hit += layerSummary.Lookups.Hit
		summary.Totals.Lookups.Miss += layerSummary.Lookups.Miss
		summary.Totals.Lookups.Error += layerSummary.Lookups.Error
		summary.Totals.Lookups.Skip += layerSummary.Lookups.Skip
		summary.Totals.Lookups.Total += layerSummary.Lookups.Total
		summary.Totals.Writes.Success += layerSummary.Writes.Success
		summary.Totals.Writes.Error += layerSummary.Writes.Error
		summary.Totals.Writes.Total += layerSummary.Writes.Total
	}
	summary.Totals.HitRate = hitRate(summary.Totals.Lookups)
	summary.Totals.LookupAvgMs = avgDurationMs(s.lookupDuration)
	summary.Totals.ValueAvgBytes = avgValueBytes(s.valueSize)
	return summary
}

func (s *cacheStats) layerSummaryLocked(layer string) CacheLayerSummary {
	lookups := lookupSummary(s.lookups[layer])
	writes := writeSummary(s.writes[layer])
	return CacheLayerSummary{
		Layer:         layer,
		Lookups:       lookups,
		Writes:        writes,
		HitRate:       hitRate(lookups),
		LookupAvgMs:   durationAvgMs(s.lookupDuration[layer]),
		ValueAvgBytes: valueAvgBytes(s.valueSize[layer]),
	}
}

func lookupSummary(values map[string]int64) CacheLookupSummary {
	out := CacheLookupSummary{
		Hit:   values["hit"],
		Miss:  values["miss"],
		Error: values["error"],
		Skip:  values["skip"],
	}
	out.Total = out.Hit + out.Miss + out.Error + out.Skip
	return out
}

func writeSummary(values map[string]int64) CacheWriteSummary {
	out := CacheWriteSummary{
		Success: values["success"],
		Error:   values["error"] + values["failure"],
	}
	out.Total = out.Success + out.Error
	return out
}

func hitRate(values CacheLookupSummary) float64 {
	if values.Total == 0 {
		return 0
	}
	return float64(values.Hit) / float64(values.Total)
}

func durationAvgMs(stats cacheDurationStats) float64 {
	if stats.count == 0 {
		return 0
	}
	return float64(stats.total) / float64(stats.count) / float64(time.Millisecond)
}

func valueAvgBytes(stats cacheValueStats) float64 {
	if stats.count == 0 {
		return 0
	}
	return float64(stats.total) / float64(stats.count)
}

func avgDurationMs(values map[string]cacheDurationStats) float64 {
	var count int64
	var total time.Duration
	for _, stats := range values {
		count += stats.count
		total += stats.total
	}
	if count == 0 {
		return 0
	}
	return float64(total) / float64(count) / float64(time.Millisecond)
}

func avgValueBytes(values map[string]cacheValueStats) float64 {
	var count int64
	var total int64
	for _, stats := range values {
		count += stats.count
		total += stats.total
	}
	if count == 0 {
		return 0
	}
	return float64(total) / float64(count)
}

func (m *Metrics) IncEventBusDropped() {
	if m == nil || !m.enabled {
		return
	}
	m.eventBusDropped.Inc()
}

func (m *Metrics) IncEventBusProcessed() {
	if m == nil || !m.enabled {
		return
	}
	m.eventBusProcessed.Inc()
}

func (m *Metrics) IncEventBusPanics() {
	if m == nil || !m.enabled {
		return
	}
	m.eventBusPanics.Inc()
}

func (m *Metrics) SetEventBusQueueSize(size int) {
	if m == nil || !m.enabled {
		return
	}
	m.eventBusQueueSize.Set(float64(size))
}

func (m *Metrics) SetInferenceScraper(scraper *router.InferenceScraper) {
	if m == nil {
		return
	}
	m.scraperMu.Lock()
	defer m.scraperMu.Unlock()
	m.inferenceScraper = scraper
}

func (m *Metrics) inferenceScraperGet(name string) (router.InferenceState, bool) {
	m.scraperMu.RLock()
	defer m.scraperMu.RUnlock()
	if m.inferenceScraper == nil {
		return router.InferenceState{}, false
	}
	return m.inferenceScraper.Get(name)
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

		if inf, ok := m.inferenceScraperGet(item.Name); ok {
			m.providerGPUCacheUsage.WithLabelValues(providerLabel).Set(inf.GPUCacheUsagePerc)
			m.providerCPUCacheUsage.WithLabelValues(providerLabel).Set(inf.CPUCacheUsagePerc)
			m.providerCacheHitRate.WithLabelValues(providerLabel).Set(inf.CacheHitRate())
			m.providerCacheQueries.WithLabelValues(providerLabel).Set(inf.CacheQueryTotal)
			m.providerCacheHits.WithLabelValues(providerLabel).Set(inf.CacheQueryHit)
		}
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
