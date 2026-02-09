package router

import (
	"context"
	"errors"
	"sync"
	"time"
)

// RoutingStrategy defines how to select a provider
type RoutingStrategy string

const (
	StrategyRoundRobin    RoutingStrategy = "round-robin"
	StrategyLeastLatency  RoutingStrategy = "least-latency"
	StrategyWeighted      RoutingStrategy = "weighted"
	StrategyCostOptimized RoutingStrategy = "cost-optimized"
	StrategyPriority      RoutingStrategy = "priority"
)

// ProviderHealth tracks the health status of a provider
type ProviderHealth struct {
	Name            string
	Healthy         bool
	LastCheck       time.Time
	LastError       error
	ConsecutiveFails int
	AvgLatency      time.Duration
	TotalRequests   int64
	FailedRequests  int64
	mu              sync.RWMutex
}

// ProviderWeight defines weighted routing configuration
type ProviderWeight struct {
	Name   string
	Weight int // Higher weight = more traffic
}

// ProviderCost defines cost per 1M tokens
type ProviderCost struct {
	Name        string
	InputCost   float64 // USD per 1M input tokens
	OutputCost  float64 // USD per 1M output tokens
}

// RoutingConfig defines routing behavior
type RoutingConfig struct {
	Strategy       RoutingStrategy
	Providers      []string          // List of provider names
	Fallback       []string          // Fallback order
	Weights        []ProviderWeight  // For weighted strategy
	Costs          []ProviderCost    // For cost-optimized strategy
	CustomRules    []*CustomRule     // User-defined routing rules
	HealthCheck    HealthCheckConfig
	RetryPolicy    RetryConfig
	CircuitBreaker CircuitBreakerConfig
}

// HealthCheckConfig defines health check behavior
type HealthCheckConfig struct {
	Enabled           bool
	Interval          time.Duration
	Timeout           time.Duration
	UnhealthyThreshold int // Consecutive failures before marking unhealthy
	HealthyThreshold   int // Consecutive successes before marking healthy
}

// RetryConfig defines retry behavior
type RetryConfig struct {
	Enabled     bool
	MaxRetries  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64 // Exponential backoff multiplier
}

// CircuitBreakerConfig defines circuit breaker behavior
type CircuitBreakerConfig struct {
	Enabled           bool
	FailureThreshold  int           // Number of failures before opening circuit
	SuccessThreshold  int           // Number of successes before closing circuit
	Timeout           time.Duration // How long to wait before trying again
	HalfOpenRequests  int           // Number of requests to allow in half-open state
}

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	state            CircuitState
	failureCount     int
	successCount     int
	lastFailureTime  time.Time
	config           CircuitBreakerConfig
	mu               sync.RWMutex
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		state:  CircuitClosed,
		config: config,
	}
}

// Allow checks if a request should be allowed
func (cb *CircuitBreaker) Allow() error {
	if !cb.config.Enabled {
		return nil
	}

	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case CircuitOpen:
		if time.Since(cb.lastFailureTime) > cb.config.Timeout {
			// Transition to half-open
			cb.mu.RUnlock()
			cb.mu.Lock()
			cb.state = CircuitHalfOpen
			cb.successCount = 0
			cb.mu.Unlock()
			cb.mu.RLock()
			return nil
		}
		return errors.New("circuit breaker is open")
	case CircuitHalfOpen:
		// Allow limited requests in half-open state
		return nil
	default:
		return nil
	}
}

// RecordSuccess records a successful request
func (cb *CircuitBreaker) RecordSuccess() {
	if !cb.config.Enabled {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitHalfOpen:
		cb.successCount++
		if cb.successCount >= cb.config.SuccessThreshold {
			cb.state = CircuitClosed
			cb.failureCount = 0
		}
	case CircuitClosed:
		cb.failureCount = 0
	}
}

// RecordFailure records a failed request
func (cb *CircuitBreaker) RecordFailure() {
	if !cb.config.Enabled {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()
	cb.failureCount++

	switch cb.state {
	case CircuitHalfOpen:
		cb.state = CircuitOpen
		cb.successCount = 0
	case CircuitClosed:
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.state = CircuitOpen
		}
	}
}

// GetState returns the current circuit state
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// MarkHealthy marks a provider as healthy
func (ph *ProviderHealth) MarkHealthy(latency time.Duration) {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	ph.Healthy = true
	ph.LastCheck = time.Now()
	ph.ConsecutiveFails = 0
	ph.TotalRequests++

	// Update average latency with exponential moving average
	if ph.AvgLatency == 0 {
		ph.AvgLatency = latency
	} else {
		ph.AvgLatency = time.Duration(float64(ph.AvgLatency)*0.7 + float64(latency)*0.3)
	}
}

// MarkUnhealthy marks a provider as unhealthy
func (ph *ProviderHealth) MarkUnhealthy(err error) {
	ph.mu.Lock()
	defer ph.mu.Unlock()

	ph.ConsecutiveFails++
	ph.LastCheck = time.Now()
	ph.LastError = err
	ph.TotalRequests++
	ph.FailedRequests++
}

// IsHealthy returns whether the provider is healthy
func (ph *ProviderHealth) IsHealthy() bool {
	ph.mu.RLock()
	defer ph.mu.RUnlock()
	return ph.Healthy
}

// GetLatency returns the average latency
func (ph *ProviderHealth) GetLatency() time.Duration {
	ph.mu.RLock()
	defer ph.mu.RUnlock()
	return ph.AvgLatency
}

// GetStats returns provider statistics
func (ph *ProviderHealth) GetStats() (total, failed int64, avgLatency time.Duration) {
	ph.mu.RLock()
	defer ph.mu.RUnlock()
	return ph.TotalRequests, ph.FailedRequests, ph.AvgLatency
}

// ProviderSelector selects a provider based on strategy
type ProviderSelector interface {
	Select(ctx context.Context) (string, error)
	RecordSuccess(provider string, latency time.Duration)
	RecordFailure(provider string, err error)
	GetHealthyProviders() []string
}
