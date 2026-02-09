package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// NewHealthChecker creates a new health checker
func NewHealthChecker(mcpURL string, config HealthCheckConfig) *HealthChecker {
	return &HealthChecker{
		mcpURL:   mcpURL,
		config:   config,
		healthy:  true,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

// Start starts the health check loop
func (hc *HealthChecker) Start() {
	defer close(hc.doneChan)

	ticker := time.NewTicker(hc.config.Interval)
	defer ticker.Stop()

	// Perform initial health check
	hc.performHealthCheck()

	for {
		select {
		case <-ticker.C:
			hc.performHealthCheck()
		case <-hc.stopChan:
			return
		}
	}
}

// Stop stops the health checker
func (hc *HealthChecker) Stop() {
	close(hc.stopChan)
	<-hc.doneChan
}

// IsHealthy returns whether the MCP server is healthy
func (hc *HealthChecker) IsHealthy() bool {
	hc.mu.RLock()
	defer hc.mu.RUnlock()
	return hc.healthy
}

// performHealthCheck performs a single health check
func (hc *HealthChecker) performHealthCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), hc.config.Timeout)
	defer cancel()

	// Build health check URL
	healthURL := hc.mcpURL
	if hc.config.Endpoint != "" {
		healthURL = hc.mcpURL + hc.config.Endpoint
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		hc.recordFailure(err)
		return
	}

	// Execute request
	client := &http.Client{
		Timeout: hc.config.Timeout,
	}
	resp, err := client.Do(req)
	if err != nil {
		hc.recordFailure(err)
		return
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		hc.recordSuccess()
	} else {
		hc.recordFailure(fmt.Errorf("health check returned status %d", resp.StatusCode))
	}
}

// recordSuccess records a successful health check
func (hc *HealthChecker) recordSuccess() {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.lastCheck = time.Now()
	hc.consecutiveFails = 0
	hc.consecutiveSuccess++

	// Mark as healthy if threshold met
	if hc.consecutiveSuccess >= hc.config.HealthyThreshold {
		if !hc.healthy {
			slog.Info("MCP server marked healthy", "url", hc.mcpURL)
		}
		hc.healthy = true
	}
}

// recordFailure records a failed health check
func (hc *HealthChecker) recordFailure(err error) {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.lastCheck = time.Now()
	hc.consecutiveFails++
	hc.consecutiveSuccess = 0

	// Mark as unhealthy if threshold met
	if hc.consecutiveFails >= hc.config.UnhealthyThreshold {
		if hc.healthy {
			slog.Error("MCP server marked unhealthy",
				"url", hc.mcpURL,
				"consecutive_fails", hc.consecutiveFails,
				"error", err,
			)
		}
		hc.healthy = false
	}
}

// GetStats returns health check statistics
func (hc *HealthChecker) GetStats() map[string]interface{} {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	return map[string]interface{}{
		"healthy":             hc.healthy,
		"last_check":          hc.lastCheck,
		"consecutive_fails":   hc.consecutiveFails,
		"consecutive_success": hc.consecutiveSuccess,
	}
}

// NewCircuitBreaker creates a new circuit breaker for MCP
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
		return fmt.Errorf("circuit breaker is open")
	case CircuitHalfOpen:
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
			slog.Info("circuit breaker closed")
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
		slog.Warn("circuit breaker opened from half-open")
		cb.state = CircuitOpen
		cb.successCount = 0
	case CircuitClosed:
		if cb.failureCount >= cb.config.FailureThreshold {
			slog.Warn("circuit breaker opened",
				"failure_count", cb.failureCount,
				"threshold", cb.config.FailureThreshold,
			)
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

// NewConnectionPool creates a new connection pool
func NewConnectionPool(mcpURL string, config ConnectionPoolConfig) (*ConnectionPool, error) {
	pool := &ConnectionPool{
		mcpURL:      mcpURL,
		config:      config,
		connections: make(chan *MCPConnection, config.MaxConnections),
	}

	// Pre-create some connections
	initialConnections := config.MaxConnections / 2
	if initialConnections < 1 {
		initialConnections = 1
	}

	for i := 0; i < initialConnections; i++ {
		conn := pool.createConnection()
		pool.connections <- conn
	}

	slog.Info("connection pool created",
		"url", mcpURL,
		"max_connections", config.MaxConnections,
		"initial_connections", initialConnections,
	)

	return pool, nil
}

// Get gets a connection from the pool
func (cp *ConnectionPool) Get(ctx context.Context) (*MCPConnection, error) {
	select {
	case conn := <-cp.connections:
		// Check if connection is still valid
		if cp.isConnectionValid(conn) {
			conn.lastUsed = time.Now()
			return conn, nil
		}
		// Connection expired, create a new one
		return cp.createConnection(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// No available connections, check if we can create more
		cp.mu.Lock()
		if cp.active < cp.config.MaxConnections {
			cp.active++
			cp.mu.Unlock()
			return cp.createConnection(), nil
		}
		cp.mu.Unlock()

		// Wait for a connection to become available
		select {
		case conn := <-cp.connections:
			if cp.isConnectionValid(conn) {
				conn.lastUsed = time.Now()
				return conn, nil
			}
			return cp.createConnection(), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Put returns a connection to the pool
func (cp *ConnectionPool) Put(conn *MCPConnection) {
	if conn == nil {
		return
	}

	// Check if connection is still valid
	if !cp.isConnectionValid(conn) {
		cp.mu.Lock()
		cp.active--
		cp.mu.Unlock()
		return
	}

	select {
	case cp.connections <- conn:
		// Successfully returned to pool
	default:
		// Pool is full, discard connection
		cp.mu.Lock()
		cp.active--
		cp.mu.Unlock()
	}
}

// createConnection creates a new connection
func (cp *ConnectionPool) createConnection() *MCPConnection {
	return &MCPConnection{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		createdAt: time.Now(),
		lastUsed:  time.Now(),
		healthy:   true,
	}
}

// isConnectionValid checks if a connection is still valid
func (cp *ConnectionPool) isConnectionValid(conn *MCPConnection) bool {
	if !conn.healthy {
		return false
	}

	// Check max lifetime
	if cp.config.MaxLifetime > 0 && time.Since(conn.createdAt) > cp.config.MaxLifetime {
		return false
	}

	// Check max idle time
	if cp.config.MaxIdleTime > 0 && time.Since(conn.lastUsed) > cp.config.MaxIdleTime {
		return false
	}

	return true
}

// Close closes the connection pool
func (cp *ConnectionPool) Close() error {
	close(cp.connections)
	return nil
}

// GetStats returns pool statistics
func (cp *ConnectionPool) GetStats() map[string]interface{} {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	return map[string]interface{}{
		"active_connections":    cp.active,
		"available_connections": len(cp.connections),
		"max_connections":       cp.config.MaxConnections,
	}
}
