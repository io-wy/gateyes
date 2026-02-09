package router

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"time"
)

// RetryHandler wraps a smart router with retry logic
type RetryHandler struct {
	router      *SmartRouter
	retryConfig RetryConfig
}

// NewRetryHandler creates a new retry handler
func NewRetryHandler(router *SmartRouter, config RetryConfig) *RetryHandler {
	return &RetryHandler{
		router:      router,
		retryConfig: config,
	}
}

// ExecuteWithRetry executes a request with retry logic
func (rh *RetryHandler) ExecuteWithRetry(
	ctx context.Context,
	executeFunc func(provider string) error,
) error {
	if !rh.retryConfig.Enabled {
		// No retry, just execute once
		provider, err := rh.router.SelectWithFallback(ctx)
		if err != nil {
			return err
		}
		return executeFunc(provider)
	}

	var lastErr error
	delay := rh.retryConfig.InitialDelay

	for attempt := 0; attempt <= rh.retryConfig.MaxRetries; attempt++ {
		// Select provider (with fallback)
		provider, err := rh.router.SelectWithFallback(ctx)
		if err != nil {
			lastErr = err
			slog.Error("failed to select provider",
				"attempt", attempt+1,
				"error", err,
			)

			// If we can't even select a provider, wait and try again
			if attempt < rh.retryConfig.MaxRetries {
				time.Sleep(delay)
				delay = rh.calculateNextDelay(delay)
			}
			continue
		}

		// Execute the request
		startTime := time.Now()
		err = executeFunc(provider)
		latency := time.Since(startTime)

		if err == nil {
			// Success!
			rh.router.RecordSuccess(provider, latency)
			slog.Debug("request succeeded",
				"provider", provider,
				"attempt", attempt+1,
				"latency", latency,
			)
			return nil
		}

		// Record failure
		rh.router.RecordFailure(provider, err)
		lastErr = err

		slog.Warn("request failed, will retry",
			"provider", provider,
			"attempt", attempt+1,
			"max_retries", rh.retryConfig.MaxRetries,
			"error", err,
		)

		// Check if we should retry
		if attempt < rh.retryConfig.MaxRetries {
			// Check if error is retryable
			if !isRetryableError(err) {
				slog.Info("error is not retryable, stopping",
					"error", err,
				)
				return err
			}

			// Wait before retry with exponential backoff
			time.Sleep(delay)
			delay = rh.calculateNextDelay(delay)
		}
	}

	return lastErr
}

// calculateNextDelay calculates the next delay using exponential backoff
func (rh *RetryHandler) calculateNextDelay(currentDelay time.Duration) time.Duration {
	nextDelay := time.Duration(float64(currentDelay) * rh.retryConfig.Multiplier)
	if nextDelay > rh.retryConfig.MaxDelay {
		return rh.retryConfig.MaxDelay
	}
	return nextDelay
}

// isRetryableError determines if an error should trigger a retry
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Network errors are retryable
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// Context errors are not retryable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// HTTP status codes that are retryable
	// 429 (Too Many Requests), 500, 502, 503, 504
	// This would need to be checked by the caller and wrapped in a specific error type

	return true
}

// HTTPRetryableError wraps an HTTP status code for retry logic
type HTTPRetryableError struct {
	StatusCode int
	Message    string
}

func (e *HTTPRetryableError) Error() string {
	return e.Message
}

// IsRetryableHTTPStatus checks if an HTTP status code is retryable
func IsRetryableHTTPStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// NewHTTPRetryableError creates a new HTTP retryable error
func NewHTTPRetryableError(statusCode int, message string) error {
	if IsRetryableHTTPStatus(statusCode) {
		return &HTTPRetryableError{
			StatusCode: statusCode,
			Message:    message,
		}
	}
	return errors.New(message)
}

// CalculateBackoff calculates exponential backoff with jitter
func CalculateBackoff(attempt int, initialDelay, maxDelay time.Duration, multiplier float64) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	// Calculate exponential backoff
	backoff := float64(initialDelay) * math.Pow(multiplier, float64(attempt))

	// Cap at max delay
	if backoff > float64(maxDelay) {
		backoff = float64(maxDelay)
	}

	// Add jitter (±25%)
	jitter := backoff * 0.25 * (2*float64(time.Now().UnixNano()%100)/100.0 - 1)

	return time.Duration(backoff + jitter)
}
