package cache

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache stores entries in Redis as JSON-encoded blobs. It is the
// preferred backend for multi-replica deployments because it lets every
// gateway pod see the same cache state. Network failures are surfaced
// to the caller as errors so the caller can decide whether to fall back
// to a secondary cache; counters are still updated so degradation is
// visible in /metrics.
//
// The client is injected — RedisCache does NOT own the *redis.Client
// and therefore Close() is a no-op. This avoids tearing down a shared
// connection pool that other components (limiter, dedup, etc.) depend on.
type RedisCache struct {
	client *redis.Client
	defTTL time.Duration
	hits   atomic.Uint64
	misses atomic.Uint64
	writes atomic.Uint64
	errs   atomic.Uint64
}

// RedisConfig configures the Redis-backed cache. The Redis connection
// itself is provided as a *redis.Client so it can be shared with the
// limiter, dedup, etc.
type RedisConfig struct {
	// DefaultTTL applies when Set is called with ttl<=0. Zero ⇒ persist
	// indefinitely (Redis SET without EX). Production should always set
	// a finite TTL — cached upstream responses go stale.
	DefaultTTL time.Duration
}

// NewRedisCache wires a RedisCache around an existing *redis.Client.
// The caller retains ownership of the client.
func NewRedisCache(client *redis.Client, cfg RedisConfig) *RedisCache {
	return &RedisCache{client: client, defTTL: cfg.DefaultTTL}
}

// Get returns (entry, true, nil) on hit, (nil, false, nil) on a clean
// miss (redis.Nil), and (nil, false, err) when the backend is degraded.
// Callers should treat the err case as a soft miss and proceed to the
// upstream — the caller-side fallback (FallbackCache) handles that.
func (c *RedisCache) Get(ctx context.Context, key string) (*Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if c.client == nil {
		c.errs.Add(1)
		return nil, false, errors.New("cache: redis client nil")
	}
	raw, err := c.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		c.misses.Add(1)
		return nil, false, nil
	}
	if err != nil {
		c.errs.Add(1)
		return nil, false, err
	}
	var en Entry
	if err := json.Unmarshal(raw, &en); err != nil {
		c.errs.Add(1)
		return nil, false, err
	}
	c.hits.Add(1)
	return &en, true, nil
}

// Set marshals the entry as JSON and writes it with the chosen TTL.
// ttl<=0 falls back to DefaultTTL; DefaultTTL==0 ⇒ no expiry.
func (c *RedisCache) Set(ctx context.Context, key string, e *Entry, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e == nil {
		return errors.New("cache: nil entry")
	}
	if c.client == nil {
		c.errs.Add(1)
		return errors.New("cache: redis client nil")
	}
	if ttl <= 0 {
		ttl = c.defTTL
	}
	raw, err := json.Marshal(e)
	if err != nil {
		c.errs.Add(1)
		return err
	}
	if err := c.client.Set(ctx, key, raw, ttl).Err(); err != nil {
		c.errs.Add(1)
		return err
	}
	c.writes.Add(1)
	return nil
}

// Delete removes the entry. Missing key is not an error (Redis DEL is
// already idempotent — it just reports 0 affected).
func (c *RedisCache) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.client == nil {
		c.errs.Add(1)
		return errors.New("cache: redis client nil")
	}
	if err := c.client.Del(ctx, key).Err(); err != nil {
		c.errs.Add(1)
		return err
	}
	return nil
}

// Stats returns a snapshot of aggregate counters since process start.
// Counters use atomics so this is lock-free.
func (c *RedisCache) Stats() Stats {
	return Stats{
		Hits:   c.hits.Load(),
		Misses: c.misses.Load(),
		Writes: c.writes.Load(),
		Errors: c.errs.Load(),
	}
}

// Close is a deliberate no-op: the *redis.Client is shared with other
// subsystems (limiter, dedup) and they manage its lifecycle.
func (c *RedisCache) Close() error { return nil }
