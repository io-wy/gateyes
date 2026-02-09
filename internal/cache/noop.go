package cache

import (
	"context"
	"time"
)

// NoOpCache is a cache that does nothing (for when caching is disabled)
type NoOpCache struct{}

// NewNoOpCache creates a new no-op cache
func NewNoOpCache() *NoOpCache {
	return &NoOpCache{}
}

// Get always returns cache miss
func (nc *NoOpCache) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, ErrCacheMiss
}

// Set does nothing
func (nc *NoOpCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return nil
}

// Delete does nothing
func (nc *NoOpCache) Delete(ctx context.Context, key string) error {
	return nil
}

// Clear does nothing
func (nc *NoOpCache) Clear(ctx context.Context) error {
	return nil
}

// Stats returns empty statistics
func (nc *NoOpCache) Stats() CacheStats {
	return CacheStats{}
}
