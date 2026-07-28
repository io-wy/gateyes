// Package retry provides a tiny, context-aware retry helper.
//
// It is intentionally minimal: no external dependencies, deterministic backoff,
// and cancellation support. Use it for best-effort recovery of transient
// failures in asynchronous or background paths where a full retry library
// would be overkill.
package retry

import (
	"context"
	"time"
)

// Do calls fn until it succeeds, the context is cancelled, or maxAttempts is
// exhausted. The delay between attempt i and i+1 is baseDelay * 2^i, capped by
// the context deadline.
//
// Example:
//
//	err := retry.Do(ctx, 3, 50*time.Millisecond, func() error {
//	    return db.Exec(...)
//	})
func Do(ctx context.Context, maxAttempts int, baseDelay time.Duration, fn func() error) error {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	var lastErr error
	for i := 0; i < maxAttempts; i++ {
		if lastErr = fn(); lastErr == nil {
			return nil
		}
		if i == maxAttempts-1 {
			break
		}
		delay := baseDelay * (1 << i)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return lastErr
}
