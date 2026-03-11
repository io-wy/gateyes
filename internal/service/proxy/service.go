package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gateyes/internal/config"
	"gateyes/internal/provider"
	providerfactory "gateyes/internal/provider/factory"
	"gateyes/internal/requestmeta"
	usagex "gateyes/internal/service/usage"
)

type OpenAIProxy struct {
	providers      *provider.Registry
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
	circuitEnabled   bool
	failureThreshold int
	successThreshold int
	openTimeout      time.Duration
	halfOpenRequests int

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

type providerCircuitState int

const (
	circuitClosed providerCircuitState = iota
	circuitOpen
	circuitHalfOpen
)

type providerRuntime struct {
	avgLatency         time.Duration
	totalRequests      int64
	failedRequests     int64
	consecutiveFailure int
	consecutiveSuccess int
	unhealthyUntil     time.Time
	state              providerCircuitState
	halfOpenRemaining  int
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

type responseUsage = usagex.TokenUsage

func NewOpenAIProxy(
	cfg config.GatewayConfig,
	authCfg config.AuthConfig,
	providers map[string]config.ProviderConfig,
) (*OpenAIProxy, error) {
	registry, err := providerfactory.NewModelRegistry(providers)
	if err != nil {
		return nil, err
	}

	providerNames := registry.Names()
	providerProfiles := make(map[string]providerProfile, len(providers))
	for name, provider := range providers {
		normalized := normalizeProviderName(name)
		if normalized == "" {
			continue
		}
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

	if len(providerNames) == 0 {
		slog.Warn("no providers configured")
	}

	defaultProvider := normalizeProviderName(cfg.DefaultProvider)
	if defaultProvider == "" && len(providerNames) > 0 {
		defaultProvider = providerNames[0]
	}
	if defaultProvider != "" {
		if !registry.Has(defaultProvider) && len(providerNames) > 0 {
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

	failureThreshold := cfg.Routing.HealthCheck.UnhealthyThreshold
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	successThreshold := cfg.Routing.HealthCheck.HealthyThreshold
	if successThreshold <= 0 {
		successThreshold = 1
	}
	openTimeout := markUnhealthyFor
	halfOpenRequests := 1
	circuitEnabled := cfg.Routing.CircuitBreaker.Enabled
	if circuitEnabled {
		if cfg.Routing.CircuitBreaker.FailureThreshold > 0 {
			failureThreshold = cfg.Routing.CircuitBreaker.FailureThreshold
		} else {
			failureThreshold = 5
		}
		if cfg.Routing.CircuitBreaker.SuccessThreshold > 0 {
			successThreshold = cfg.Routing.CircuitBreaker.SuccessThreshold
		} else {
			successThreshold = 2
		}
		openTimeout = cfg.Routing.CircuitBreaker.Timeout.Duration
		if openTimeout <= 0 {
			openTimeout = markUnhealthyFor
		}
		halfOpenRequests = cfg.Routing.CircuitBreaker.HalfOpenRequests
		if halfOpenRequests <= 0 {
			halfOpenRequests = successThreshold
		}
		if halfOpenRequests < 1 {
			halfOpenRequests = 1
		}
	}

	stats := make(map[string]*providerRuntime, len(providerNames))
	for _, providerName := range providerNames {
		stats[providerName] = &providerRuntime{
			state: circuitClosed,
		}
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
		circuitEnabled:   circuitEnabled,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		openTimeout:      openTimeout,
		halfOpenRequests: halfOpenRequests,
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
	if model != "" {
		r.Header.Set(requestmeta.HeaderResolvedModel, model)
	}
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
		r.Header.Set(requestmeta.HeaderStreamRequest, "1")
		o.proxyStreaming(w, r, body, providersOrder)
		return
	}

	r.Header.Del(requestmeta.HeaderStreamRequest)
	o.proxyWithFallback(w, r, body, providersOrder)
}

type streamProviderResult struct {
	attempts       int
	statusCode     int
	circuitOpens   int64
	shouldFallback bool
	handled        bool
}

func (o *OpenAIProxy) proxyStreaming(
	w http.ResponseWriter,
	r *http.Request,
	body []byte,
	providersOrder []string,
) {
	totalRetries := int64(0)
	totalFallbacks := int64(0)
	totalCircuitOpens := int64(0)

	for index, provider := range providersOrder {
		allowFallback := index < len(providersOrder)-1
		result := o.executeStreamingProvider(w, r, body, provider, allowFallback)
		totalRetries += int64(maxInt(result.attempts-1, 0))
		totalCircuitOpens += result.circuitOpens

		if result.shouldFallback && allowFallback {
			totalFallbacks++
			slog.Warn(
				"stream fallback to next provider",
				"failed_provider", provider,
				"attempts", result.attempts,
				"status", result.statusCode,
				"next_provider", providersOrder[index+1],
				"path", r.URL.Path,
			)
			continue
		}

		r.Header.Set(requestmeta.HeaderRetryCount, strconv.FormatInt(totalRetries, 10))
		r.Header.Set(requestmeta.HeaderFallbackCount, strconv.FormatInt(totalFallbacks, 10))
		r.Header.Set(requestmeta.HeaderCircuitOpenCount, strconv.FormatInt(totalCircuitOpens, 10))
		if provider != "" {
			r.Header.Set(requestmeta.HeaderResolvedProvider, provider)
		}
		if result.handled {
			return
		}
	}

	r.Header.Set(requestmeta.HeaderRetryCount, strconv.FormatInt(totalRetries, 10))
	r.Header.Set(requestmeta.HeaderFallbackCount, strconv.FormatInt(totalFallbacks, 10))
	r.Header.Set(requestmeta.HeaderCircuitOpenCount, strconv.FormatInt(totalCircuitOpens, 10))
	writeGatewayError(w, http.StatusBadGateway, "all providers failed")
}

func (o *OpenAIProxy) executeStreamingProvider(
	w http.ResponseWriter,
	r *http.Request,
	body []byte,
	provider string,
	allowFallback bool,
) streamProviderResult {
	result := streamProviderResult{}
	proxy, ok := o.providers.Get(provider)
	if !ok {
		if allowFallback {
			result.shouldFallback = true
			result.statusCode = http.StatusBadGateway
		} else {
			writeGatewayError(w, http.StatusBadGateway, "unknown provider")
			result.handled = true
		}
		return result
	}

	delay := o.initialDelay
	maxAttempts := o.maxRetryAttempts()
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result.attempts = attempt

		start := time.Now()
		resp, cancel, upstreamErr := proxy.ForwardRequest(r, body)
		if upstreamErr != nil {
			cancel()
			result.statusCode = upstreamErrorStatus(r.Context(), upstreamErr)
			if opened := o.observeProvider(provider, result.statusCode, time.Since(start)); opened {
				result.circuitOpens++
			}

			if errors.Is(r.Context().Err(), context.Canceled) || errors.Is(upstreamErr, context.Canceled) {
				result.handled = true
				return result
			}

			if attempt < maxAttempts && shouldFallback(result.statusCode) {
				slog.Warn(
					"retry on same provider (stream)",
					"provider", provider,
					"attempt", attempt,
					"max_attempts", maxAttempts,
					"status", result.statusCode,
					"next_delay_ms", delay.Milliseconds(),
					"path", r.URL.Path,
				)
				if !waitRetryDelay(r.Context(), delay) {
					if allowFallback {
						result.shouldFallback = true
						return result
					}
					result.handled = true
					return result
				}
				delay = nextRetryDelay(delay, o.maxDelay, o.retryMultiplier)
				continue
			}

			if allowFallback {
				result.shouldFallback = true
				return result
			}
			writeGatewayError(w, result.statusCode, "upstream unavailable")
			result.handled = true
			return result
		}

		result.statusCode = resp.StatusCode
		if opened := o.observeProvider(provider, resp.StatusCode, time.Since(start)); opened {
			result.circuitOpens++
		}

		if shouldFallback(resp.StatusCode) && attempt < maxAttempts {
			slog.Warn(
				"retry on same provider (stream)",
				"provider", provider,
				"attempt", attempt,
				"max_attempts", maxAttempts,
				"status", resp.StatusCode,
				"next_delay_ms", delay.Milliseconds(),
				"path", r.URL.Path,
			)
			drainAndClose(resp.Body)
			cancel()
			if !waitRetryDelay(r.Context(), delay) {
				if allowFallback {
					result.shouldFallback = true
					return result
				}
				result.handled = true
				return result
			}
			delay = nextRetryDelay(delay, o.maxDelay, o.retryMultiplier)
			continue
		}

		if shouldFallback(resp.StatusCode) && allowFallback {
			drainAndClose(resp.Body)
			cancel()
			result.shouldFallback = true
			return result
		}

		copyResponseHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)

		streaming := isStreamingResponse(resp)
		usage, usageFound, copyErr := copyResponseBodyWithUsage(w, resp.Body, r.Context(), streaming)
		if copyErr != nil && !errors.Is(copyErr, context.Canceled) {
			slog.Debug("stream proxy copy interrupted", "error", copyErr, "provider", provider, "path", r.URL.Path)
		}

		drainAndClose(resp.Body)
		cancel()
		if usageFound {
			attachUsageHeaders(r, usage)
		}
		result.handled = true
		return result
	}

	return result
}

func (o *OpenAIProxy) proxyWithFallback(
	w http.ResponseWriter,
	r *http.Request,
	body []byte,
	providersOrder []string,
) {
	totalRetries := int64(0)
	totalFallbacks := int64(0)
	totalCircuitOpens := int64(0)

	for index, provider := range providersOrder {
		buffered, attempts, circuitOpens, ok := o.executeProviderWithRetry(provider, r, body)
		if !ok {
			continue
		}
		totalRetries += int64(maxInt(attempts-1, 0))
		totalCircuitOpens += circuitOpens

		if shouldFallback(buffered.statusCode) && index < len(providersOrder)-1 {
			totalFallbacks++
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

		r.Header.Set(requestmeta.HeaderRetryCount, strconv.FormatInt(totalRetries, 10))
		r.Header.Set(requestmeta.HeaderFallbackCount, strconv.FormatInt(totalFallbacks, 10))
		r.Header.Set(requestmeta.HeaderCircuitOpenCount, strconv.FormatInt(totalCircuitOpens, 10))
		o.attachResponseMetadata(r, provider, buffered)
		buffered.CopyTo(w)
		return
	}

	r.Header.Set(requestmeta.HeaderRetryCount, strconv.FormatInt(totalRetries, 10))
	r.Header.Set(requestmeta.HeaderFallbackCount, strconv.FormatInt(totalFallbacks, 10))
	r.Header.Set(requestmeta.HeaderCircuitOpenCount, strconv.FormatInt(totalCircuitOpens, 10))
	writeGatewayError(w, http.StatusBadGateway, "all providers failed")
}

func (o *OpenAIProxy) executeProviderWithRetry(
	provider string,
	r *http.Request,
	body []byte,
) (*bufferedResponse, int, int64, bool) {
	proxy, ok := o.providers.Get(provider)
	if !ok {
		return nil, 0, 0, false
	}

	maxAttempts := o.maxRetryAttempts()

	delay := o.initialDelay
	var buffered *bufferedResponse
	circuitOpens := int64(0)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cloned := cloneRequestWithBody(r, body)
		buffered = newBufferedResponse()

		start := time.Now()
		proxy.ServeHTTP(buffered, cloned)
		if opened := o.observeProvider(provider, buffered.statusCode, time.Since(start)); opened {
			circuitOpens++
		}

		if !shouldFallback(buffered.statusCode) || attempt == maxAttempts {
			return buffered, attempt, circuitOpens, true
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
			return buffered, attempt, circuitOpens, true
		}
		delay = nextRetryDelay(delay, o.maxDelay, o.retryMultiplier)
	}

	return buffered, maxAttempts, circuitOpens, true
}

func (o *OpenAIProxy) maxRetryAttempts() int {
	maxAttempts := 1
	if o.retryEnabled {
		maxAttempts = o.maxRetries + 1
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return maxAttempts
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
	jitterRange := float64(next) * 0.2
	jitter := (rand.Float64()*2 - 1) * jitterRange
	candidate := time.Duration(float64(next) + jitter)
	if candidate <= 0 {
		return next
	}
	if candidate > maxDelay {
		return maxDelay
	}
	return candidate
}

func (o *OpenAIProxy) resolveProviderOrder(r *http.Request, model string) ([]string, error) {
	profile := o.routingProfileForRequest(r)
	if len(profile.providers) == 0 {
		return nil, errors.New("no provider configured")
	}

	explicit := o.resolveProviderFromRequest(r)
	if explicit != "" {
		if !o.providers.Has(explicit) {
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
	providers *provider.Registry,
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
		if !providers.Has(provider) {
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
	bestProvider := ""
	bestLatency := time.Duration(1<<63 - 1)

	for _, provider := range candidates {
		if o.isProviderTemporarilyUnhealthy(provider) {
			continue
		}
		latency := o.providerLatency(provider)
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

func (o *OpenAIProxy) providerLatency(provider string) time.Duration {
	o.statsMu.RLock()
	defer o.statsMu.RUnlock()

	stats, ok := o.stats[provider]
	if !ok {
		return 0
	}
	return stats.avgLatency
}

func (o *OpenAIProxy) isProviderTemporarilyUnhealthy(provider string) bool {
	o.statsMu.Lock()
	defer o.statsMu.Unlock()

	stats, ok := o.stats[provider]
	if !ok {
		return false
	}

	now := time.Now()
	if !o.circuitEnabled {
		return now.Before(stats.unhealthyUntil)
	}

	switch stats.state {
	case circuitOpen:
		if now.Before(stats.unhealthyUntil) {
			return true
		}
		stats.state = circuitHalfOpen
		stats.consecutiveSuccess = 0
		stats.halfOpenRemaining = o.halfOpenRequests
		return false
	case circuitHalfOpen:
		if stats.halfOpenRemaining <= 0 {
			_ = o.openProviderCircuitLocked(stats, now, o.openTimeout)
			return true
		}
		return false
	default:
		return false
	}
}

func (o *OpenAIProxy) IsProviderTemporarilyUnhealthy(provider string) bool {
	return o.isProviderTemporarilyUnhealthy(provider)
}

func (o *OpenAIProxy) observeProvider(provider string, status int, latency time.Duration) bool {
	o.statsMu.Lock()
	defer o.statsMu.Unlock()

	stats, ok := o.stats[provider]
	if !ok {
		stats = &providerRuntime{}
		o.stats[provider] = stats
	}
	now := time.Now()

	stats.totalRequests++
	if shouldFallback(status) {
		stats.failedRequests++
		stats.consecutiveFailure++
		stats.consecutiveSuccess = 0

		if !o.circuitEnabled {
			if stats.consecutiveFailure >= o.failureThreshold {
				return o.openProviderCircuitLocked(stats, now, o.markUnhealthyFor)
			}
			return false
		}

		switch stats.state {
		case circuitHalfOpen:
			return o.openProviderCircuitLocked(stats, now, o.openTimeout)
		case circuitOpen:
			stats.unhealthyUntil = now.Add(o.openTimeout)
			return false
		default:
			if stats.consecutiveFailure >= o.failureThreshold {
				return o.openProviderCircuitLocked(stats, now, o.openTimeout)
			}
			return false
		}
	}

	stats.consecutiveFailure = 0
	if stats.avgLatency <= 0 {
		stats.avgLatency = latency
	} else {
		stats.avgLatency = time.Duration(float64(stats.avgLatency)*0.7 + float64(latency)*0.3)
	}

	if !o.circuitEnabled {
		stats.unhealthyUntil = time.Time{}
		stats.state = circuitClosed
		stats.halfOpenRemaining = 0
		stats.consecutiveSuccess = 0
		return false
	}

	switch stats.state {
	case circuitHalfOpen:
		if stats.halfOpenRemaining > 0 {
			stats.halfOpenRemaining--
		}
		stats.consecutiveSuccess++
		if stats.consecutiveSuccess >= o.successThreshold {
			stats.state = circuitClosed
			stats.unhealthyUntil = time.Time{}
			stats.halfOpenRemaining = 0
			stats.consecutiveSuccess = 0
			return false
		}
		if stats.halfOpenRemaining <= 0 {
			return o.openProviderCircuitLocked(stats, now, o.openTimeout)
		}
	case circuitOpen:
		if now.After(stats.unhealthyUntil) {
			stats.state = circuitHalfOpen
			stats.halfOpenRemaining = o.halfOpenRequests
			stats.consecutiveSuccess = 0
		}
	default:
		stats.consecutiveSuccess = 0
	}

	return false
}

func (o *OpenAIProxy) openProviderCircuitLocked(
	stats *providerRuntime,
	now time.Time,
	timeout time.Duration,
) bool {
	if stats == nil {
		return false
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	wasOpen := stats.state == circuitOpen && now.Before(stats.unhealthyUntil)
	stats.state = circuitOpen
	stats.unhealthyUntil = now.Add(timeout)
	stats.consecutiveSuccess = 0
	stats.halfOpenRemaining = 0
	return !wasOpen
}

func normalizeProviderList(
	candidates []string,
	providers *provider.Registry,
) []string {
	seen := make(map[string]struct{}, len(candidates))
	normalized := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		name := normalizeProviderName(candidate)
		if name == "" {
			continue
		}
		if !providers.Has(name) {
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
	providers *provider.Registry,
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
		strings.HasSuffix(clean, "/responses"),
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

func upstreamErrorStatus(ctx context.Context, err error) int {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return 499
	}
	return http.StatusBadGateway
}

func copyResponseBodyWithUsage(
	dst http.ResponseWriter,
	src io.Reader,
	ctx context.Context,
	flush bool,
) (responseUsage, bool, error) {
	if !flush {
		payload, err := io.ReadAll(src)
		if len(payload) > 0 {
			if _, writeErr := dst.Write(payload); writeErr != nil {
				return responseUsage{}, false, writeErr
			}
		}
		if err != nil {
			return responseUsage{}, false, err
		}
		usage, ok := extractUsageFromBody(payload)
		return usage, ok, nil
	}

	flusher, ok := dst.(http.Flusher)
	if !ok {
		_, err := io.Copy(dst, src)
		return responseUsage{}, false, err
	}

	parser := &streamUsageParser{}
	buffer := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return parser.usage, parser.found, ctx.Err()
		default:
		}

		n, err := src.Read(buffer)
		if n > 0 {
			chunk := buffer[:n]
			if _, writeErr := dst.Write(chunk); writeErr != nil {
				return parser.usage, parser.found, writeErr
			}
			parser.ingest(chunk)
			flusher.Flush()
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				parser.finalize()
				return parser.usage, parser.found, nil
			}
			return parser.usage, parser.found, err
		}
	}
}

type streamUsageParser struct {
	pending bytes.Buffer
	usage   responseUsage
	found   bool
}

func (p *streamUsageParser) ingest(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	_, _ = p.pending.Write(chunk)
	for {
		line, ok := p.nextLine()
		if !ok {
			return
		}
		p.consumeLine(line)
	}
}

func (p *streamUsageParser) finalize() {
	if p.pending.Len() == 0 {
		return
	}
	line := strings.TrimSpace(p.pending.String())
	p.pending.Reset()
	p.consumeLine(line)
}

func (p *streamUsageParser) nextLine() (string, bool) {
	data := p.pending.Bytes()
	index := bytes.IndexByte(data, '\n')
	if index < 0 {
		return "", false
	}

	line := strings.TrimSpace(string(data[:index]))
	p.pending.Next(index + 1)
	return line, true
}

func (p *streamUsageParser) consumeLine(line string) {
	if line == "" || strings.HasPrefix(line, ":") {
		return
	}
	if !strings.HasPrefix(line, "data:") {
		return
	}

	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return
	}

	usage, ok := extractUsageFromBody([]byte(payload))
	if !ok {
		return
	}
	p.usage = usage
	p.found = true
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

func incrementHeaderCounter(r *http.Request, key string, delta int64) {
	if r == nil || delta <= 0 {
		return
	}
	current := int64(0)
	if raw := strings.TrimSpace(r.Header.Get(key)); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil && value > 0 {
			current = value
		}
	}
	r.Header.Set(key, strconv.FormatInt(current+delta, 10))
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func (o *OpenAIProxy) attachResponseMetadata(r *http.Request, provider string, response *bufferedResponse) {
	if r == nil || response == nil {
		return
	}

	if provider != "" {
		r.Header.Set(requestmeta.HeaderResolvedProvider, provider)
	}

	if usage, ok := extractUsageFromBody(response.body.Bytes()); ok {
		attachUsageHeaders(r, usage)
	}
}

func attachUsageHeaders(r *http.Request, usage responseUsage) {
	usagex.AttachToHeaders(r, usage)
}

func extractUsageFromBody(body []byte) (responseUsage, bool) {
	return usagex.ExtractResponse(body)
}

func drainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 64*1024))
	_ = body.Close()
}
