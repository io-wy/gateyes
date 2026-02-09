package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// MCPGuard provides protection and monitoring for MCP connections
type MCPGuard struct {
	config          GuardConfig
	healthCheckers  map[string]*HealthChecker
	circuitBreakers map[string]*CircuitBreaker
	connectionPools map[string]*ConnectionPool
	metrics         *MCPMetrics
	mu              sync.RWMutex
}

// GuardConfig defines MCP protection configuration
type GuardConfig struct {
	Enabled            bool
	HealthCheck        HealthCheckConfig
	CircuitBreaker     CircuitBreakerConfig
	Timeout            TimeoutConfig
	RateLimit          RateLimitConfig
	ConnectionPool     ConnectionPoolConfig
	AnomalyDetection   AnomalyDetectionConfig
	FallbackBehavior   FallbackBehavior
}

// HealthCheckConfig defines health check settings
type HealthCheckConfig struct {
	Enabled            bool
	Interval           time.Duration
	Timeout            time.Duration
	HealthyThreshold   int
	UnhealthyThreshold int
	Endpoint           string // Health check endpoint path
}

// CircuitBreakerConfig defines circuit breaker settings
type CircuitBreakerConfig struct {
	Enabled          bool
	FailureThreshold int
	SuccessThreshold int
	Timeout          time.Duration
	HalfOpenRequests int
}

// TimeoutConfig defines timeout settings
type TimeoutConfig struct {
	Connect time.Duration
	Read    time.Duration
	Write   time.Duration
	Idle    time.Duration
}

// RateLimitConfig defines rate limiting for MCP requests
type RateLimitConfig struct {
	Enabled           bool
	RequestsPerSecond int
	Burst             int
}

// ConnectionPoolConfig defines connection pool settings
type ConnectionPoolConfig struct {
	Enabled         bool
	MaxConnections  int
	MaxIdleTime     time.Duration
	MaxLifetime     time.Duration
	HealthCheckFreq time.Duration
}

// AnomalyDetectionConfig defines anomaly detection settings
type AnomalyDetectionConfig struct {
	Enabled              bool
	ErrorRateThreshold   float64 // e.g., 0.5 = 50% error rate
	LatencyThreshold     time.Duration
	ConsecutiveErrors    int
	AlertWebhook         string
}

// FallbackBehavior defines what to do when MCP fails
type FallbackBehavior struct {
	Strategy      string // "fail", "cache", "mock", "alternative"
	CacheTTL      time.Duration
	AlternativeMCP string
}

// HealthChecker monitors MCP server health
type HealthChecker struct {
	mcpURL           string
	config           HealthCheckConfig
	healthy          bool
	lastCheck        time.Time
	consecutiveFails int
	consecutiveSuccess int
	mu               sync.RWMutex
	stopChan         chan struct{}
	doneChan         chan struct{}
}

// CircuitBreaker implements circuit breaker pattern for MCP
type CircuitBreaker struct {
	state            CircuitState
	failureCount     int
	successCount     int
	lastFailureTime  time.Time
	config           CircuitBreakerConfig
	mu               sync.RWMutex
}

// CircuitState represents circuit breaker state
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// ConnectionPool manages connections to MCP servers
type ConnectionPool struct {
	mcpURL      string
	config      ConnectionPoolConfig
	connections chan *MCPConnection
	active      int
	mu          sync.RWMutex
}

// MCPConnection represents a connection to an MCP server
type MCPConnection struct {
	client    *http.Client
	createdAt time.Time
	lastUsed  time.Time
	healthy   bool
}

// MCPMetrics tracks MCP performance metrics
type MCPMetrics struct {
	totalRequests    int64
	failedRequests   int64
	totalLatency     time.Duration
	lastErrorTime    time.Time
	lastError        error
	errorRate        float64
	mu               sync.RWMutex
}

// NewMCPGuard creates a new MCP guard
func NewMCPGuard(config GuardConfig) *MCPGuard {
	return &MCPGuard{
		config:          config,
		healthCheckers:  make(map[string]*HealthChecker),
		circuitBreakers: make(map[string]*CircuitBreaker),
		connectionPools: make(map[string]*ConnectionPool),
		metrics:         &MCPMetrics{},
	}
}

// RegisterMCP registers an MCP server for protection
func (mg *MCPGuard) RegisterMCP(mcpURL string) error {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	if !mg.config.Enabled {
		return nil
	}

	// Create health checker
	if mg.config.HealthCheck.Enabled {
		hc := NewHealthChecker(mcpURL, mg.config.HealthCheck)
		mg.healthCheckers[mcpURL] = hc
		go hc.Start()
	}

	// Create circuit breaker
	if mg.config.CircuitBreaker.Enabled {
		mg.circuitBreakers[mcpURL] = NewCircuitBreaker(mg.config.CircuitBreaker)
	}

	// Create connection pool
	if mg.config.ConnectionPool.Enabled {
		pool, err := NewConnectionPool(mcpURL, mg.config.ConnectionPool)
		if err != nil {
			return fmt.Errorf("failed to create connection pool: %w", err)
		}
		mg.connectionPools[mcpURL] = pool
	}

	slog.Info("MCP server registered with guard",
		"url", mcpURL,
		"health_check", mg.config.HealthCheck.Enabled,
		"circuit_breaker", mg.config.CircuitBreaker.Enabled,
	)

	return nil
}

// ExecuteRequest executes a request to MCP with protection
func (mg *MCPGuard) ExecuteRequest(
	ctx context.Context,
	mcpURL string,
	executeFunc func(client *http.Client) error,
) error {
	if !mg.config.Enabled {
		// No protection, execute directly
		return executeFunc(http.DefaultClient)
	}

	// Check health
	if !mg.IsHealthy(mcpURL) {
		return mg.handleUnhealthy(mcpURL)
	}

	// Check circuit breaker
	mg.mu.RLock()
	cb, hasCB := mg.circuitBreakers[mcpURL]
	mg.mu.RUnlock()

	if hasCB {
		if err := cb.Allow(); err != nil {
			return mg.handleCircuitOpen(mcpURL)
		}
	}

	// Get connection from pool or use default client
	var client *http.Client
	mg.mu.RLock()
	pool, hasPool := mg.connectionPools[mcpURL]
	mg.mu.RUnlock()

	if hasPool {
		conn, err := pool.Get(ctx)
		if err != nil {
			return fmt.Errorf("failed to get connection: %w", err)
		}
		defer pool.Put(conn)
		client = conn.client
	} else {
		client = http.DefaultClient
	}

	// Execute with timeout
	ctx, cancel := context.WithTimeout(ctx, mg.config.Timeout.Read)
	defer cancel()

	// Execute and track metrics
	startTime := time.Now()
	err := executeFunc(client)
	latency := time.Since(startTime)

	// Record metrics
	mg.recordMetrics(mcpURL, err, latency)

	// Update circuit breaker
	if hasCB {
		if err != nil {
			cb.RecordFailure()
		} else {
			cb.RecordSuccess()
		}
	}

	// Check for anomalies
	if mg.config.AnomalyDetection.Enabled {
		mg.detectAnomalies(mcpURL)
	}

	return err
}

// IsHealthy checks if an MCP server is healthy
func (mg *MCPGuard) IsHealthy(mcpURL string) bool {
	mg.mu.RLock()
	hc, exists := mg.healthCheckers[mcpURL]
	mg.mu.RUnlock()

	if !exists {
		return true // No health checker, assume healthy
	}

	return hc.IsHealthy()
}

// handleUnhealthy handles requests to unhealthy MCP servers
func (mg *MCPGuard) handleUnhealthy(mcpURL string) error {
	switch mg.config.FallbackBehavior.Strategy {
	case "fail":
		return fmt.Errorf("MCP server %s is unhealthy", mcpURL)
	case "alternative":
		if mg.config.FallbackBehavior.AlternativeMCP != "" {
			slog.Warn("falling back to alternative MCP",
				"original", mcpURL,
				"alternative", mg.config.FallbackBehavior.AlternativeMCP,
			)
			// Caller should retry with alternative
			return fmt.Errorf("MCP server unhealthy, use alternative: %s",
				mg.config.FallbackBehavior.AlternativeMCP)
		}
		return fmt.Errorf("MCP server %s is unhealthy and no alternative configured", mcpURL)
	default:
		return fmt.Errorf("MCP server %s is unhealthy", mcpURL)
	}
}

// handleCircuitOpen handles requests when circuit is open
func (mg *MCPGuard) handleCircuitOpen(mcpURL string) error {
	slog.Warn("circuit breaker is open for MCP", "url", mcpURL)
	return fmt.Errorf("circuit breaker is open for MCP server %s", mcpURL)
}

// recordMetrics records request metrics
func (mg *MCPGuard) recordMetrics(mcpURL string, err error, latency time.Duration) {
	mg.metrics.mu.Lock()
	defer mg.metrics.mu.Unlock()

	mg.metrics.totalRequests++
	mg.metrics.totalLatency += latency

	if err != nil {
		mg.metrics.failedRequests++
		mg.metrics.lastError = err
		mg.metrics.lastErrorTime = time.Now()
	}

	// Calculate error rate
	if mg.metrics.totalRequests > 0 {
		mg.metrics.errorRate = float64(mg.metrics.failedRequests) / float64(mg.metrics.totalRequests)
	}
}

// detectAnomalies detects anomalous behavior
func (mg *MCPGuard) detectAnomalies(mcpURL string) {
	mg.metrics.mu.RLock()
	errorRate := mg.metrics.errorRate
	avgLatency := time.Duration(0)
	if mg.metrics.totalRequests > 0 {
		avgLatency = mg.metrics.totalLatency / time.Duration(mg.metrics.totalRequests)
	}
	mg.metrics.mu.RUnlock()

	// Check error rate threshold
	if errorRate > mg.config.AnomalyDetection.ErrorRateThreshold {
		slog.Error("MCP error rate threshold exceeded",
			"url", mcpURL,
			"error_rate", errorRate,
			"threshold", mg.config.AnomalyDetection.ErrorRateThreshold,
		)
		mg.sendAlert(mcpURL, fmt.Sprintf("Error rate %.2f%% exceeds threshold", errorRate*100))
	}

	// Check latency threshold
	if avgLatency > mg.config.AnomalyDetection.LatencyThreshold {
		slog.Warn("MCP latency threshold exceeded",
			"url", mcpURL,
			"avg_latency", avgLatency,
			"threshold", mg.config.AnomalyDetection.LatencyThreshold,
		)
		mg.sendAlert(mcpURL, fmt.Sprintf("Average latency %v exceeds threshold", avgLatency))
	}
}

// sendAlert sends an alert about MCP issues
func (mg *MCPGuard) sendAlert(mcpURL, message string) {
	if mg.config.AnomalyDetection.AlertWebhook == "" {
		return
	}

	slog.Info("sending MCP alert",
		"url", mcpURL,
		"message", message,
		"webhook", mg.config.AnomalyDetection.AlertWebhook,
	)

	// TODO: Implement webhook alert sending
}

// GetMetrics returns current metrics
func (mg *MCPGuard) GetMetrics(mcpURL string) map[string]interface{} {
	mg.metrics.mu.RLock()
	defer mg.metrics.mu.RUnlock()

	avgLatency := time.Duration(0)
	if mg.metrics.totalRequests > 0 {
		avgLatency = mg.metrics.totalLatency / time.Duration(mg.metrics.totalRequests)
	}

	return map[string]interface{}{
		"total_requests":  mg.metrics.totalRequests,
		"failed_requests": mg.metrics.failedRequests,
		"error_rate":      mg.metrics.errorRate,
		"avg_latency_ms":  avgLatency.Milliseconds(),
		"last_error":      mg.metrics.lastError,
		"last_error_time": mg.metrics.lastErrorTime,
		"healthy":         mg.IsHealthy(mcpURL),
	}
}

// Close cleans up resources
func (mg *MCPGuard) Close() error {
	mg.mu.Lock()
	defer mg.mu.Unlock()

	// Stop health checkers
	for _, hc := range mg.healthCheckers {
		hc.Stop()
	}

	// Close connection pools
	for _, pool := range mg.connectionPools {
		pool.Close()
	}

	return nil
}
