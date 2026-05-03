package cache

import (
	"context"
	"time"
)

// FallbackCache wraps a primary Cache with a secondary fallback.
// On Get: primary hit → return; primary miss/error → try fallback.
// On Set: write to both; return primary's error (fallback write is best-effort).
// On Delete: delete from both; always return nil (non-blocking).
//
// The intended use-case is Redis primary + MemoryCache fallback so that
// brief Redis outages still serve warm entries from memory instead of
// hitting upstream every time.
type FallbackCache struct {
	primary  Cache
	fallback Cache
}

// NewFallbackCache returns a cache that tries primary first and falls
// back to secondary on misses or errors.
func NewFallbackCache(primary, fallback Cache) *FallbackCache {
	return &FallbackCache{primary: primary, fallback: fallback}
}

// Get tries primary, then fallback on miss or error.
func (c *FallbackCache) Get(ctx context.Context, key string) (*Entry, bool, error) {
	if c.primary != nil {
		entry, hit, err := c.primary.Get(ctx, key)
		if hit {
			return entry, true, nil
		}
		if c.fallback != nil {
			return c.fallback.Get(ctx, key)
		}
		return entry, hit, err
	}
	if c.fallback != nil {
		return c.fallback.Get(ctx, key)
	}
	return nil, false, nil
}

// Set writes to both primary and fallback. Returns primary's error;
// fallback write is fire-and-forget.
func (c *FallbackCache) Set(ctx context.Context, key string, e *Entry, ttl time.Duration) error {
	var primaryErr error
	if c.primary != nil {
		primaryErr = c.primary.Set(ctx, key, e, ttl)
	}
	if c.fallback != nil {
		_ = c.fallback.Set(ctx, key, e, ttl)
	}
	return primaryErr
}

// Delete removes from both caches. Never returns an error.
func (c *FallbackCache) Delete(ctx context.Context, key string) error {
	if c.primary != nil {
		_ = c.primary.Delete(ctx, key)
	}
	if c.fallback != nil {
		_ = c.fallback.Delete(ctx, key)
	}
	return nil
}

// Stats returns the primary cache's stats.
func (c *FallbackCache) Stats() Stats {
	if c.primary != nil {
		return c.primary.Stats()
	}
	if c.fallback != nil {
		return c.fallback.Stats()
	}
	return Stats{}
}

// Close closes both caches.
func (c *FallbackCache) Close() error {
	if c.primary != nil {
		_ = c.primary.Close()
	}
	if c.fallback != nil {
		_ = c.fallback.Close()
	}
	return nil
}
