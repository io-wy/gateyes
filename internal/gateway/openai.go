package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gateyes/internal/config"
	"gateyes/internal/requestmeta"
)

type OpenAIProxy struct {
	providers      map[string]*UpstreamProxy
	providerNames  []string
	providerHeader string
	providerQuery  string

	defaultProvider  string
	routingEnabled   bool
	strategy         string
	fallback         []string
	modelRules       []modelRouteRule
	markUnhealthyFor time.Duration
	retryEnabled     bool
	maxRetries       int
	initialDelay     time.Duration
	maxDelay         time.Duration
	retryMultiplier  float64

	virtualKeys      map[string]config.VirtualKeyConfig
	providerProfiles map[string]providerProfile
	rrCounter        uint64

	statsMu sync.RWMutex
	stats   map[string]*providerRuntime
}

type modelRouteRule struct {
	name     string
	priority int
	operator string
	value    string
	provider string
	pattern  *regexp.Regexp
}

type providerRuntime struct {
	avgLatency         time.Duration
	totalRequests      int64
	failedRequests     int64
	consecutiveFailure int
	unhealthyUntil     time.Time
}

type providerProfile struct {
	weight     int
	inputCost  float64
	outputCost float64
}

type routingProfile struct {
	providers       []string
	strategy        string
	fallback        []string
	defaultProvider string
	modelRules      []modelRouteRule
}

func NewOpenAIProxy(
	cfg config.GatewayConfig,
	authCfg config.AuthConfig,
	providers map[string]config.ProviderConfig,
) (*OpenAIProxy, error) {
	registry := make(map[string]*UpstreamProxy)
	providerNames := make([]string, 0, len(providers))
	providerProfiles := make(map[string]providerProfile, len(providers))
	for name, provider := range providers {
		normalized := normalizeProviderName(name)
		if normalized == "" {
			continue
		}
		proxy, err := NewUpstreamProxy(provider.BaseURL, provider.WSBaseURL, provider.Headers, provider.AuthHeader, provider.AuthScheme, provider.APIKey, "")
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}
		registry[normalized] = proxy
		providerNames = append(providerNames, normalized)
		weight := provider.Weight
		if weight <= 0 {
			weight = 1
		}
		providerProfiles[normalized] = providerProfile{
			weight:     weight,
			inputCost:  provider.InputCost,
			outputCost: provider.OutputCost,
		}
	}
	sort.Strings(providerNames)

	if len(registry) == 0 {
		slog.Warn("no providers configured")
	}

	defaultProvider := normalizeProviderName(cfg.DefaultProvider)
	if defaultProvider == "" && len(providerNames) > 0 {
		defaultProvider = providerNames[0]
	}
	if defaultProvider != "" {
		if _, ok := registry[defaultProvider]; !ok && len(providerNames) > 0 {
			defaultProvider = providerNames[0]
		}
	}

	markUnhealthyFor := cfg.Routing.HealthCheck.Interval.Duration
	if markUnhealthyFor <= 0 {
		markUnhealthyFor = 30 * time.Second
	}

	maxRetries := cfg.Routing.Retry.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	initialDelay := cfg.Routing.Retry.InitialDelay.Duration
	if initialDelay <= 0 {
		initialDelay = 100 * time.Millisecond
	}

	maxDelay := cfg.Routing.Retry.MaxDelay.Duration
	if maxDelay <= 0 {
		maxDelay = 2 * time.Second
	}
	if maxDelay < initialDelay {
		maxDelay = initialDelay
	}

	retryMultiplier := cfg.Routing.Retry.Multiplier
	if retryMultiplier < 1.1 {
		retryMultiplier = 2.0
	}

	stats := make(map[string]*providerRuntime, len(providerNames))
	for _, providerName := range providerNames {
		stats[providerName] = &providerRuntime{}
	}

	virtualKeys := normalizeVirtualKeys(authCfg.VirtualKeys, registry)

	return &OpenAIProxy{
		providers:        registry,
		providerNames:    providerNames,
		providerHeader:   cfg.ProviderHeader,
		providerQuery:    cfg.ProviderQuery,
		defaultProvider:  defaultProvider,
		routingEnabled:   cfg.Routing.Enabled,
		strategy:         strings.ToLower(strings.TrimSpace(cfg.Routing.Strategy)),
		fallback:         normalizeProviderList(cfg.Routing.Fallback, registry),
		modelRules:       buildModelRules(cfg.Routing.CustomRules, registry),
		markUnhealthyFor: markUnhealthyFor,
		retryEnabled:     cfg.Routing.Retry.Enabled,
		maxRetries:       maxRetries,
		initialDelay:     initialDelay,
		maxDelay:         maxDelay,
		retryMultiplier:  retryMultiplier,
		virtualKeys:      virtualKeys,
		providerProfiles: providerProfiles,
		stats:            stats,
	}, nil
}

func (o *OpenAIProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !supportsOpenAIPath(r.URL.Path) {
		writeGatewayError(w, http.StatusNotFound, "unsupported OpenAI endpoint")
		return
	}

	body, err := readAndRestoreBody(r)
	if err != nil {
		writeGatewayError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	model, stream := extractModelHints(body)
	providersOrder, err := o.resolveProviderOrder(r, model)
	if err != nil {
		writeGatewayError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(providersOrder) == 0 {
		writeGatewayError(w, http.StatusServiceUnavailable, "no available providers")
		return
	}

	if stream {
		o.proxyStreaming(w, r, body, providersOrder[0])
		return
	}

	o.proxyWithFallback(w, r, body, providersOrder)
}

func (o *OpenAIProxy) proxyStreaming(w http.ResponseWriter, r *http.Request, body []byte, provider string) {
	proxy, ok := o.providers[provider]
	if !ok {
		writeGatewayError(w, http.StatusBadRequest, "unknown provider")
		return
	}

	recorder := &statusCaptureWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	start := time.Now()
	proxy.ServeHTTP(recorder, cloneRequestWithBody(r, body))
	o.observeProvider(provider, recorder.statusCode, time.Since(start))
}

func (o *OpenAIProxy) proxyWithFallback(
	w http.ResponseWriter,
	r *http.Request,
	body []byte,
	providersOrder []string,
) {
	for index, provider := range providersOrder {
		buffered, attempts, ok := o.executeProviderWithRetry(provider, r, body)
		if !ok {
			continue
		}

		if shouldFallback(buffered.statusCode) && index < len(providersOrder)-1 {
			slog.Warn(
				"fallback to next provider",
				"failed_provider", provider,
				"attempts", attempts,
				"status", buffered.statusCode,
				"next_provider", providersOrder[index+1],
				"path", r.URL.Path,
			)
			continue
		}

		buffered.CopyTo(w)
		return
	}

	writeGatewayError(w, http.StatusBadGateway, "all providers failed")
}

func (o *OpenAIProxy) executeProviderWithRetry(
	provider string,
	r *http.Request,
	body []byte,
) (*bufferedResponse, int, bool) {
	proxy, ok := o.providers[provider]
	if !ok {
		return nil, 0, false
	}

	maxAttempts := 1
	if o.retryEnabled {
		maxAttempts = o.maxRetries + 1
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	delay := o.initialDelay
	var buffered *bufferedResponse

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cloned := cloneRequestWithBody(r, body)
		buffered = newBufferedResponse()

		start := time.Now()
		proxy.ServeHTTP(buffered, cloned)
		o.observeProvider(provider, buffered.statusCode, time.Since(start))

		if !shouldFallback(buffered.statusCode) || attempt == maxAttempts {
			return buffered, attempt, true
		}

		slog.Warn(
			"retry on same provider",
			"provider", provider,
			"attempt", attempt,
			"max_attempts", maxAttempts,
			"status", buffered.statusCode,
			"next_delay_ms", delay.Milliseconds(),
			"path", r.URL.Path,
		)

		if !waitRetryDelay(r.Context(), delay) {
			return buffered, attempt, true
		}
		delay = nextRetryDelay(delay, o.maxDelay, o.retryMultiplier)
	}

	return buffered, maxAttempts, true
}

func waitRetryDelay(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func nextRetryDelay(current, maxDelay time.Duration, multiplier float64) time.Duration {
	next := time.Duration(float64(current) * multiplier)
	if next > maxDelay {
		return maxDelay
	}
	if next <= 0 {
		return maxDelay
	}
	return next
}

func (o *OpenAIProxy) resolveProviderOrder(r *http.Request, model string) ([]string, error) {
	profile := o.routingProfileForRequest(r)
	if len(profile.providers) == 0 {
		return nil, errors.New("no provider configured")
	}

	explicit := o.resolveProviderFromRequest(r)
	if explicit != "" {
		if _, ok := o.providers[explicit]; !ok {
			return nil, fmt.Errorf("unknown provider %q", explicit)
		}
		if !containsProvider(profile.providers, explicit) {
			return nil, fmt.Errorf("provider %q is not allowed by virtual key", explicit)
		}
		return o.buildProviderOrder(explicit, profile), nil
	}

	if matched := matchModelRule(profile.modelRules, model); matched != "" {
		return o.buildProviderOrder(matched, profile), nil
	}

	primary := o.selectPrimaryProvider(profile)
	if primary == "" {
		primary = profile.defaultProvider
	}
	if primary == "" && len(profile.providers) > 0 {
		primary = profile.providers[0]
	}
	return o.buildProviderOrder(primary, profile), nil
}

func (o *OpenAIProxy) resolveProviderFromRequest(r *http.Request) string {
	if o.providerHeader != "" {
		if value := strings.TrimSpace(r.Header.Get(o.providerHeader)); value != "" {
			return normalizeProviderName(value)
		}
	}
	if o.providerQuery != "" {
		if value := strings.TrimSpace(r.URL.Query().Get(o.providerQuery)); value != "" {
			return normalizeProviderName(value)
		}
	}
	return ""
}

func (o *OpenAIProxy) buildProviderOrder(primary string, profile routingProfile) []string {
	order := make([]string, 0, len(profile.providers))
	seen := make(map[string]struct{}, len(profile.providers))
	allowed := make(map[string]struct{}, len(profile.providers))
	for _, providerName := range profile.providers {
		allowed[providerName] = struct{}{}
	}

	add := func(name string, skipUnhealthy bool) {
		normalized := normalizeProviderName(name)
		if normalized == "" {
			return
		}
		if _, ok := allowed[normalized]; !ok {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		if skipUnhealthy && o.isProviderTemporarilyUnhealthy(normalized) {
			return
		}
		seen[normalized] = struct{}{}
		order = append(order, normalized)
	}

	add(primary, false)
	for _, fallbackProvider := range profile.fallback {
		add(fallbackProvider, true)
	}
	for _, providerName := range profile.providers {
		add(providerName, true)
	}
	for _, providerName := range profile.providers {
		add(providerName, false)
	}

	return order
}

func matchModelRule(rules []modelRouteRule, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	for _, rule := range rules {
		if rule.Match(model) {
			return rule.provider
		}
	}
	return ""
}

func (o *OpenAIProxy) routingProfileForRequest(r *http.Request) routingProfile {
	providers := append([]string(nil), o.providerNames...)
	fallback := append([]string(nil), o.fallback...)
	modelRules := append([]modelRouteRule(nil), o.modelRules...)
	defaultProvider := normalizeProviderName(o.defaultProvider)
	strategy := o.strategy
	if strategy == "" {
		strategy = "least-latency"
	}

	virtualKey := strings.TrimSpace(r.Header.Get(requestmeta.HeaderVirtualKey))
	if virtualKey == "" {
		return routingProfile{
			providers:       providers,
			strategy:        strategy,
			fallback:        fallback,
			defaultProvider: defaultProvider,
			modelRules:      modelRules,
		}
	}

	virtualConfig, ok := o.virtualKeys[virtualKey]
	if !ok || !virtualConfig.Enabled {
		return routingProfile{
			providers:       providers,
			strategy:        strategy,
			fallback:        fallback,
			defaultProvider: defaultProvider,
			modelRules:      modelRules,
		}
	}

	virtualProviders := normalizeProviderList(virtualConfig.Providers, o.providers)
	if len(virtualProviders) > 0 {
		providers = virtualProviders
	}

	if routingStrategy := strings.TrimSpace(virtualConfig.Routing.Strategy); routingStrategy != "" {
		strategy = strings.ToLower(routingStrategy)
	}

	if virtualDefault := normalizeProviderName(virtualConfig.DefaultProvider); virtualDefault != "" {
		defaultProvider = virtualDefault
	}
	if !containsProvider(providers, defaultProvider) && len(providers) > 0 {
		defaultProvider = providers[0]
	}

	if len(virtualConfig.Routing.Fallback) > 0 {
		fallback = normalizeProviderList(virtualConfig.Routing.Fallback, o.providers)
	}

	if len(virtualConfig.Routing.CustomRules) > 0 {
		modelRules = buildModelRules(virtualConfig.Routing.CustomRules, o.providers)
	}

	return routingProfile{
		providers:       providers,
		strategy:        strategy,
		fallback:        fallback,
		defaultProvider: defaultProvider,
		modelRules:      modelRules,
	}
}

func (o *OpenAIProxy) selectPrimaryProvider(profile routingProfile) string {
	if len(profile.providers) == 0 {
		return ""
	}
	if !o.routingEnabled {
		return profile.defaultProvider
	}

	switch profile.strategy {
	case "round-robin":
		return o.selectRoundRobinProvider(profile.providers)
	case "priority":
		return o.selectPriorityProvider(profile.providers)
	case "weighted":
		return o.selectWeightedProvider(profile.providers)
	case "cost-optimized":
		return o.selectCostOptimizedProvider(profile.providers)
	case "least-latency":
		fallthrough
	default:
		return o.selectLeastLatencyProvider(profile.providers)
	}
}

func (o *OpenAIProxy) selectRoundRobinProvider(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	index := atomic.AddUint64(&o.rrCounter, 1)
	return candidates[(index-1)%uint64(len(candidates))]
}

func (o *OpenAIProxy) selectPriorityProvider(candidates []string) string {
	for _, provider := range candidates {
		if !o.isProviderTemporarilyUnhealthy(provider) {
			return provider
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func (o *OpenAIProxy) selectWeightedProvider(candidates []string) string {
	totalWeight := 0
	for _, provider := range candidates {
		profile, ok := o.providerProfiles[provider]
		if !ok {
			totalWeight++
			continue
		}
		totalWeight += profile.weight
	}
	if totalWeight <= 0 {
		return o.selectRoundRobinProvider(candidates)
	}

	target := int(atomic.AddUint64(&o.rrCounter, 1) % uint64(totalWeight))
	current := 0
	for _, provider := range candidates {
		weight := 1
		if profile, ok := o.providerProfiles[provider]; ok && profile.weight > 0 {
			weight = profile.weight
		}
		current += weight
		if target < current {
			return provider
		}
	}
	return candidates[0]
}

func (o *OpenAIProxy) selectCostOptimizedProvider(candidates []string) string {
	bestProvider := ""
	bestCost := 1e18

	for _, provider := range candidates {
		profile, ok := o.providerProfiles[provider]
		if !ok {
			continue
		}
		cost := profile.inputCost + profile.outputCost
		if cost <= 0 {
			continue
		}
		if cost < bestCost {
			bestCost = cost
			bestProvider = provider
		}
	}

	if bestProvider != "" {
		return bestProvider
	}
	return o.selectRoundRobinProvider(candidates)
}

func (r modelRouteRule) Match(model string) bool {
	switch r.operator {
	case "equals":
		return strings.EqualFold(model, r.value)
	case "contains":
		return strings.Contains(strings.ToLower(model), strings.ToLower(r.value))
	case "starts_with":
		return strings.HasPrefix(strings.ToLower(model), strings.ToLower(r.value))
	case "ends_with":
		return strings.HasSuffix(strings.ToLower(model), strings.ToLower(r.value))
	case "regex":
		if r.pattern == nil {
			return false
		}
		return r.pattern.MatchString(model)
	default:
		return false
	}
}

func buildModelRules(
	customRules []config.CustomRule,
	providers map[string]*UpstreamProxy,
) []modelRouteRule {
	rules := make([]modelRouteRule, 0)
	for _, rule := range customRules {
		if !rule.Enabled || !strings.EqualFold(rule.Action.Type, "route") {
			continue
		}
		provider := normalizeProviderName(rule.Action.Provider)
		if provider == "" {
			continue
		}
		if _, ok := providers[provider]; !ok {
			continue
		}

		for _, condition := range rule.Conditions {
			conditionType := strings.ToLower(strings.TrimSpace(condition.Type))
			if conditionType != "body" && conditionType != "model" {
				continue
			}

			operator := strings.ToLower(strings.TrimSpace(condition.Operator))
			candidate := modelRouteRule{
				name:     rule.Name,
				priority: rule.Priority,
				operator: operator,
				value:    condition.Value,
				provider: provider,
			}

			switch operator {
			case "equals", "contains", "starts_with", "ends_with":
				if strings.TrimSpace(condition.Value) == "" {
					continue
				}
				rules = append(rules, candidate)
			case "regex":
				pattern, err := regexp.Compile(condition.Value)
				if err != nil {
					slog.Warn(
						"invalid model routing regex rule",
						"rule", rule.Name,
						"provider", provider,
						"error", err,
					)
					continue
				}
				candidate.pattern = pattern
				rules = append(rules, candidate)
			}
		}
	}

	sort.SliceStable(rules, func(i, j int) bool {
		return rules[i].priority > rules[j].priority
	})

	return rules
}

func (o *OpenAIProxy) selectLeastLatencyProvider(candidates []string) string {
	o.statsMu.RLock()
	defer o.statsMu.RUnlock()

	now := time.Now()
	bestProvider := ""
	bestLatency := time.Duration(1<<63 - 1)

	for _, provider := range candidates {
		stats, ok := o.stats[provider]
		if !ok {
			continue
		}
		if now.Before(stats.unhealthyUntil) {
			continue
		}

		latency := stats.avgLatency
		if latency <= 0 {
			latency = 500 * time.Millisecond
		}

		if latency < bestLatency {
			bestLatency = latency
			bestProvider = provider
		}
	}

	return bestProvider
}

func (o *OpenAIProxy) isProviderTemporarilyUnhealthy(provider string) bool {
	o.statsMu.RLock()
	defer o.statsMu.RUnlock()

	stats, ok := o.stats[provider]
	if !ok {
		return false
	}
	return time.Now().Before(stats.unhealthyUntil)
}

func (o *OpenAIProxy) observeProvider(provider string, status int, latency time.Duration) {
	o.statsMu.Lock()
	defer o.statsMu.Unlock()

	stats, ok := o.stats[provider]
	if !ok {
		stats = &providerRuntime{}
		o.stats[provider] = stats
	}

	stats.totalRequests++
	if shouldFallback(status) {
		stats.failedRequests++
		stats.consecutiveFailure++
		if stats.consecutiveFailure >= 3 {
			stats.unhealthyUntil = time.Now().Add(o.markUnhealthyFor)
		}
		return
	}

	stats.consecutiveFailure = 0
	stats.unhealthyUntil = time.Time{}
	if stats.avgLatency <= 0 {
		stats.avgLatency = latency
		return
	}
	stats.avgLatency = time.Duration(float64(stats.avgLatency)*0.7 + float64(latency)*0.3)
}

func normalizeProviderList(
	candidates []string,
	providers map[string]*UpstreamProxy,
) []string {
	seen := make(map[string]struct{}, len(candidates))
	normalized := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		name := normalizeProviderName(candidate)
		if name == "" {
			continue
		}
		if _, ok := providers[name]; !ok {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		normalized = append(normalized, name)
	}
	return normalized
}

func normalizeVirtualKeys(
	virtualKeys map[string]config.VirtualKeyConfig,
	providers map[string]*UpstreamProxy,
) map[string]config.VirtualKeyConfig {
	normalized := make(map[string]config.VirtualKeyConfig)
	for key, virtualConfig := range virtualKeys {
		token := strings.TrimSpace(key)
		if token == "" {
			continue
		}

		virtualConfig.Providers = normalizeProviderList(virtualConfig.Providers, providers)
		if strings.TrimSpace(virtualConfig.DefaultProvider) != "" {
			virtualConfig.DefaultProvider = normalizeProviderName(virtualConfig.DefaultProvider)
		}
		if len(virtualConfig.Routing.Fallback) > 0 {
			virtualConfig.Routing.Fallback = normalizeProviderList(virtualConfig.Routing.Fallback, providers)
		}

		normalized[token] = virtualConfig
	}
	return normalized
}

func containsProvider(providers []string, target string) bool {
	target = normalizeProviderName(target)
	for _, provider := range providers {
		if provider == target {
			return true
		}
	}
	return false
}

func supportsOpenAIPath(path string) bool {
	clean := strings.TrimSuffix(path, "/")
	switch {
	case strings.HasSuffix(clean, "/chat/completions"),
		strings.HasSuffix(clean, "/completions"),
		strings.HasSuffix(clean, "/models"),
		strings.Contains(clean, "/models/"):
		return true
	default:
		return false
	}
}

func shouldFallback(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func readAndRestoreBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPut && r.Method != http.MethodPatch {
		return nil, nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func extractModelHints(body []byte) (string, bool) {
	if len(body) == 0 {
		return "", false
	}
	var payload struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false
	}
	return strings.TrimSpace(payload.Model), payload.Stream
}

func cloneRequestWithBody(r *http.Request, body []byte) *http.Request {
	cloned := r.Clone(r.Context())
	cloned.Header.Del(requestmeta.HeaderVirtualKey)

	if len(body) > 0 {
		cloned.Body = io.NopCloser(bytes.NewReader(body))
		cloned.ContentLength = int64(len(body))
		cloned.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		}
		return cloned
	}

	cloned.Body = http.NoBody
	cloned.ContentLength = 0
	cloned.GetBody = nil
	return cloned
}

func writeGatewayError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"status":  status,
			"message": message,
		},
	})
}

type statusCaptureWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *statusCaptureWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *statusCaptureWriter) Write(payload []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	return rw.ResponseWriter.Write(payload)
}

type bufferedResponse struct {
	header     http.Header
	body       bytes.Buffer
	statusCode int
}

func newBufferedResponse() *bufferedResponse {
	return &bufferedResponse{
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (rw *bufferedResponse) Header() http.Header {
	return rw.header
}

func (rw *bufferedResponse) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
}

func (rw *bufferedResponse) Write(payload []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	return rw.body.Write(payload)
}

func (rw *bufferedResponse) CopyTo(w http.ResponseWriter) {
	for key, values := range rw.header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	statusCode := rw.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	w.WriteHeader(statusCode)
	_, _ = w.Write(rw.body.Bytes())
}

func normalizeProviderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
