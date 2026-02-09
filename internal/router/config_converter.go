package router

import (
	"gateyes/internal/config"
	"gateyes/internal/mcp"
)

// convertMCPGuardConfig converts config.MCPGuardConfig to mcp.GuardConfig
func convertMCPGuardConfig(cfg config.MCPGuardConfig) mcp.GuardConfig {
	return mcp.GuardConfig{
		Enabled: cfg.Enabled,
		HealthCheck: mcp.HealthCheckConfig{
			Enabled:            cfg.HealthCheck.Enabled,
			Interval:           cfg.HealthCheck.Interval.Duration,
			Timeout:            cfg.HealthCheck.Timeout.Duration,
			HealthyThreshold:   cfg.HealthCheck.HealthyThreshold,
			UnhealthyThreshold: cfg.HealthCheck.UnhealthyThreshold,
			Endpoint:           cfg.HealthCheck.Endpoint,
		},
		CircuitBreaker: mcp.CircuitBreakerConfig{
			Enabled:          cfg.CircuitBreaker.Enabled,
			FailureThreshold: cfg.CircuitBreaker.FailureThreshold,
			SuccessThreshold: cfg.CircuitBreaker.SuccessThreshold,
			Timeout:          cfg.CircuitBreaker.Timeout.Duration,
			HalfOpenRequests: cfg.CircuitBreaker.HalfOpenRequests,
		},
		Timeout: mcp.TimeoutConfig{
			Connect: cfg.Timeout.Connect.Duration,
			Read:    cfg.Timeout.Read.Duration,
			Write:   cfg.Timeout.Write.Duration,
			Idle:    cfg.Timeout.Idle.Duration,
		},
		ConnectionPool: mcp.ConnectionPoolConfig{
			Enabled:         cfg.ConnectionPool.Enabled,
			MaxConnections:  cfg.ConnectionPool.MaxConnections,
			MaxIdleTime:     cfg.ConnectionPool.MaxIdleTime.Duration,
			MaxLifetime:     cfg.ConnectionPool.MaxLifetime.Duration,
			HealthCheckFreq: cfg.ConnectionPool.HealthCheckFreq.Duration,
		},
		AnomalyDetection: mcp.AnomalyDetectionConfig{
			Enabled:              cfg.AnomalyDetection.Enabled,
			ErrorRateThreshold:   cfg.AnomalyDetection.ErrorRateThreshold,
			LatencyThreshold:     cfg.AnomalyDetection.LatencyThreshold.Duration,
			ConsecutiveErrors:    cfg.AnomalyDetection.ConsecutiveErrors,
			AlertWebhook:         cfg.AnomalyDetection.AlertWebhook,
		},
		FallbackBehavior: mcp.FallbackBehavior{
			Strategy:       cfg.FallbackBehavior.Strategy,
			CacheTTL:       cfg.FallbackBehavior.CacheTTL.Duration,
			AlternativeMCP: cfg.FallbackBehavior.AlternativeMCP,
		},
	}
}
