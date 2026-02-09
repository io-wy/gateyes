package router

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// SmartRouter implements intelligent provider selection with multiple strategies
type SmartRouter struct {
	config          RoutingConfig
	providers       map[string]*ProviderHealth
	circuitBreakers map[string]*CircuitBreaker
	ruleEngine      *RuleEngine
	roundRobinIndex uint64
	mu              sync.RWMutex
	healthCheckStop chan struct{}
	healthCheckDone chan struct{}
}

// NewSmartRouter creates a new smart router
func NewSmartRouter(config RoutingConfig) (*SmartRouter, error) {
	if len(config.Providers) == 0 {
		return nil, errors.New("no providers configured")
	}

	router := &SmartRouter{
		config:          config,
		providers:       make(map[string]*ProviderHealth),
		circuitBreakers: make(map[string]*CircuitBreaker),
		healthCheckStop: make(chan struct{}),
		healthCheckDone: make(chan struct{}),
	}

	// Initialize provider health tracking
	for _, name := range config.Providers {
		router.providers[name] = &ProviderHealth{
			Name:    name,
			Healthy: true,
		}
		router.circuitBreakers[name] = NewCircuitBreaker(config.CircuitBreaker)
	}

	// Initialize rule engine if custom rules are provided
	if len(config.CustomRules) > 0 {
		router.ruleEngine = NewRuleEngine(config.CustomRules)
		slog.Info("custom routing rules loaded", "count", len(config.CustomRules))
	}

	// Start health check if enabled
	if config.HealthCheck.Enabled {
		go router.runHealthChecks()
	}

	return router, nil
}

// Select chooses a provider based on the configured strategy
func (sr *SmartRouter) Select(ctx context.Context) (string, error) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	// Get healthy providers
	healthy := sr.getHealthyProvidersLocked()
	if len(healthy) == 0 {
		return "", errors.New("no healthy providers available")
	}

	// Select based on strategy
	switch sr.config.Strategy {
	case StrategyRoundRobin:
		return sr.selectRoundRobin(healthy), nil
	case StrategyLeastLatency:
		return sr.selectLeastLatency(healthy), nil
	case StrategyWeighted:
		return sr.selectWeighted(healthy), nil
	case StrategyCostOptimized:
		return sr.selectCostOptimized(healthy), nil
	case StrategyPriority:
		return sr.selectPriority(healthy), nil
	default:
		return sr.selectRoundRobin(healthy), nil
	}
}

// SelectWithRules chooses a provider based on custom rules first, then falls back to strategy
func (sr *SmartRouter) SelectWithRules(ctx context.Context, ruleCtx *RuleContext) (string, error) {
	// Try custom rules first if rule engine is configured
	if sr.ruleEngine != nil {
		action, err := sr.ruleEngine.Evaluate(ctx, ruleCtx)
		if err != nil {
			slog.Error("rule evaluation failed", "error", err)
		} else if action != nil {
			switch action.Type {
			case "route":
				// Check if the provider is healthy
				sr.mu.RLock()
				health, ok := sr.providers[action.Provider]
				sr.mu.RUnlock()

				if ok && health.IsHealthy() {
					return action.Provider, nil
				}
				slog.Warn("rule-selected provider is unhealthy, falling back to strategy",
					"provider", action.Provider,
				)
			case "reject":
				return "", fmt.Errorf("request rejected by rule: %s", action.Message)
			}
		}
	}

	// Fall back to normal strategy-based selection
	return sr.Select(ctx)
}

// selectRoundRobin implements round-robin selection
func (sr *SmartRouter) selectRoundRobin(providers []string) string {
	if len(providers) == 0 {
		return ""
	}
	index := atomic.AddUint64(&sr.roundRobinIndex, 1)
	return providers[int(index-1)%len(providers)]
}

// selectLeastLatency selects the provider with lowest average latency
func (sr *SmartRouter) selectLeastLatency(providers []string) string {
	if len(providers) == 0 {
		return ""
	}

	var bestProvider string
	var bestLatency time.Duration = time.Hour // Start with a high value

	for _, name := range providers {
		if health, ok := sr.providers[name]; ok {
			latency := health.GetLatency()
			if latency == 0 {
				// No latency data yet, give it a chance
				return name
			}
			if latency < bestLatency {
				bestLatency = latency
				bestProvider = name
			}
		}
	}

	if bestProvider == "" {
		return providers[0]
	}
	return bestProvider
}

// selectWeighted implements weighted random selection
func (sr *SmartRouter) selectWeighted(providers []string) string {
	if len(providers) == 0 {
		return ""
	}

	// Build weight map
	weights := make(map[string]int)
	totalWeight := 0
	for _, w := range sr.config.Weights {
		for _, p := range providers {
			if w.Name == p {
				weights[p] = w.Weight
				totalWeight += w.Weight
				break
			}
		}
	}

	// If no weights configured, fall back to round-robin
	if totalWeight == 0 {
		return sr.selectRoundRobin(providers)
	}

	// Weighted random selection
	r := rand.Intn(totalWeight)
	cumulative := 0
	for _, name := range providers {
		if weight, ok := weights[name]; ok {
			cumulative += weight
			if r < cumulative {
				return name
			}
		}
	}

	return providers[0]
}

// selectCostOptimized selects the cheapest provider
func (sr *SmartRouter) selectCostOptimized(providers []string) string {
	if len(providers) == 0 {
		return ""
	}

	// Build cost map
	costs := make(map[string]float64)
	for _, c := range sr.config.Costs {
		for _, p := range providers {
			if c.Name == p {
				// Use average of input and output cost as a simple metric
				costs[p] = (c.InputCost + c.OutputCost) / 2
				break
			}
		}
	}

	// If no costs configured, fall back to round-robin
	if len(costs) == 0 {
		return sr.selectRoundRobin(providers)
	}

	// Find cheapest provider
	var bestProvider string
	var bestCost float64 = 1e9 // Start with a high value

	for _, name := range providers {
		if cost, ok := costs[name]; ok {
			if cost < bestCost {
				bestCost = cost
				bestProvider = name
			}
		}
	}

	if bestProvider == "" {
		return providers[0]
	}
	return bestProvider
}

// selectPriority selects the first available provider in priority order
func (sr *SmartRouter) selectPriority(providers []string) string {
	if len(providers) == 0 {
		return ""
	}

	// Return first healthy provider in configured order
	for _, name := range sr.config.Providers {
		for _, p := range providers {
			if name == p {
				return name
			}
		}
	}

	return providers[0]
}

// RecordSuccess records a successful request
func (sr *SmartRouter) RecordSuccess(provider string, latency time.Duration) {
	sr.mu.RLock()
	health, ok := sr.providers[provider]
	cb, cbOk := sr.circuitBreakers[provider]
	sr.mu.RUnlock()

	if ok {
		health.MarkHealthy(latency)
	}
	if cbOk {
		cb.RecordSuccess()
	}

	slog.Debug("provider request succeeded",
		"provider", provider,
		"latency", latency,
	)
}

// RecordFailure records a failed request
func (sr *SmartRouter) RecordFailure(provider string, err error) {
	sr.mu.RLock()
	health, ok := sr.providers[provider]
	cb, cbOk := sr.circuitBreakers[provider]
	sr.mu.RUnlock()

	if ok {
		health.MarkUnhealthy(err)

		// Mark as unhealthy if threshold exceeded
		if sr.config.HealthCheck.Enabled &&
		   health.ConsecutiveFails >= sr.config.HealthCheck.UnhealthyThreshold {
			sr.mu.Lock()
			health.Healthy = false
			sr.mu.Unlock()
			slog.Warn("provider marked unhealthy",
				"provider", provider,
				"consecutive_fails", health.ConsecutiveFails,
			)
		}
	}
	if cbOk {
		cb.RecordFailure()
	}

	slog.Error("provider request failed",
		"provider", provider,
		"error", err,
	)
}

// GetHealthyProviders returns a list of healthy providers
func (sr *SmartRouter) GetHealthyProviders() []string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()
	return sr.getHealthyProvidersLocked()
}

// getHealthyProvidersLocked returns healthy providers (must hold read lock)
func (sr *SmartRouter) getHealthyProvidersLocked() []string {
	var healthy []string
	for _, name := range sr.config.Providers {
		if health, ok := sr.providers[name]; ok && health.IsHealthy() {
			// Check circuit breaker
			if cb, cbOk := sr.circuitBreakers[name]; cbOk {
				if cb.Allow() == nil {
					healthy = append(healthy, name)
				}
			} else {
				healthy = append(healthy, name)
			}
		}
	}
	return healthy
}

// SelectWithFallback tries to select a provider and falls back on failure
func (sr *SmartRouter) SelectWithFallback(ctx context.Context) (string, error) {
	// Try primary selection
	provider, err := sr.Select(ctx)
	if err == nil {
		return provider, nil
	}

	// Try fallback providers
	if len(sr.config.Fallback) > 0 {
		for _, fallback := range sr.config.Fallback {
			sr.mu.RLock()
			health, ok := sr.providers[fallback]
			cb, cbOk := sr.circuitBreakers[fallback]
			sr.mu.RUnlock()

			if ok && health.IsHealthy() {
				if cbOk && cb.Allow() != nil {
					continue
				}
				slog.Info("using fallback provider", "provider", fallback)
				return fallback, nil
			}
		}
	}

	return "", errors.New("no providers available (including fallbacks)")
}

// runHealthChecks periodically checks provider health
func (sr *SmartRouter) runHealthChecks() {
	defer close(sr.healthCheckDone)

	ticker := time.NewTicker(sr.config.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sr.performHealthChecks()
		case <-sr.healthCheckStop:
			return
		}
	}
}

// performHealthChecks checks all providers
func (sr *SmartRouter) performHealthChecks() {
	sr.mu.RLock()
	providers := make([]*ProviderHealth, 0, len(sr.providers))
	for _, health := range sr.providers {
		providers = append(providers, health)
	}
	sr.mu.RUnlock()

	for _, health := range providers {
		// In a real implementation, you would ping the provider here
		// For now, we'll just check if it's been failing consistently
		if health.ConsecutiveFails >= sr.config.HealthCheck.UnhealthyThreshold {
			health.mu.Lock()
			health.Healthy = false
			health.mu.Unlock()
		} else if health.ConsecutiveFails == 0 {
			health.mu.Lock()
			health.Healthy = true
			health.mu.Unlock()
		}
	}
}

// GetProviderStats returns statistics for a provider
func (sr *SmartRouter) GetProviderStats(provider string) (total, failed int64, avgLatency time.Duration, healthy bool, err error) {
	sr.mu.RLock()
	health, ok := sr.providers[provider]
	sr.mu.RUnlock()

	if !ok {
		return 0, 0, 0, false, fmt.Errorf("provider %s not found", provider)
	}

	total, failed, avgLatency = health.GetStats()
	healthy = health.IsHealthy()
	return total, failed, avgLatency, healthy, nil
}

// GetAllStats returns statistics for all providers
func (sr *SmartRouter) GetAllStats() map[string]map[string]interface{} {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	stats := make(map[string]map[string]interface{})
	for name, health := range sr.providers {
		total, failed, avgLatency := health.GetStats()
		cbState := "unknown"
		if cb, ok := sr.circuitBreakers[name]; ok {
			switch cb.GetState() {
			case CircuitClosed:
				cbState = "closed"
			case CircuitOpen:
				cbState = "open"
			case CircuitHalfOpen:
				cbState = "half-open"
			}
		}

		stats[name] = map[string]interface{}{
			"healthy":           health.IsHealthy(),
			"total_requests":    total,
			"failed_requests":   failed,
			"avg_latency_ms":    avgLatency.Milliseconds(),
			"consecutive_fails": health.ConsecutiveFails,
			"circuit_breaker":   cbState,
		}
	}
	return stats
}

// Close stops the health check goroutine
func (sr *SmartRouter) Close() error {
	if sr.config.HealthCheck.Enabled {
		close(sr.healthCheckStop)
		<-sr.healthCheckDone
	}
	return nil
}
