package cache

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

// MemoryCache is an in-memory LRU with optional per-entry TTL.
//
// It serves two roles:
//   - Primary backend in dev/test or single-node deployments where Redis
//     is not available.
//   - Fallback when RedisCache is configured but the connection is broken
//     (callers wire it via FallbackCache).
//
// Concurrency: all methods are safe for concurrent use. Lookups are O(1).
type MemoryCache struct {
	mu       sync.Mutex
	capacity int
	defTTL   time.Duration
	list     *list.List
	items    map[string]*list.Element
	stats    Stats
}

type memEntry struct {
	key       string
	value     *Entry
	expiresAt time.Time // zero ⇒ no expiration
}

// MemoryConfig configures MemoryCache behaviour.
type MemoryConfig struct {
	// Capacity is the max number of entries. Below 1 falls back to 1024.
	Capacity int
	// DefaultTTL applies when Set is called with ttl<=0. Zero ⇒ no expiry.
	DefaultTTL time.Duration
}

// NewMemoryCache returns a configured in-memory LRU cache.
func NewMemoryCache(cfg MemoryConfig) *MemoryCache {
	cap := cfg.Capacity
	if cap < 1 {
		cap = 1024
	}
	return &MemoryCache{
		capacity: cap,
		defTTL:   cfg.DefaultTTL,
		list:     list.New(),
		items:    make(map[string]*list.Element, cap),
	}
}

// Get returns the entry if present and unexpired, otherwise (nil,false,nil).
// Expired entries are evicted lazily here.
func (c *MemoryCache) Get(ctx context.Context, key string) (*Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		c.stats.Misses++
		return nil, false, nil
	}
	en := el.Value.(*memEntry)
	if !en.expiresAt.IsZero() && Now().After(en.expiresAt) {
		c.removeLocked(el)
		c.stats.Misses++
		return nil, false, nil
	}
	c.list.MoveToFront(el)
	c.stats.Hits++
	cp := *en.value
	if cp.Response != nil {
		cp.Response = append([]byte(nil), cp.Response...)
	}
	if cp.StreamRaw != nil {
		cp.StreamRaw = append([]byte(nil), cp.StreamRaw...)
	}
	return &cp, true, nil
}

// Set inserts or updates the entry. ttl<=0 uses the default; default 0 ⇒ no expiry.
func (c *MemoryCache) Set(ctx context.Context, key string, e *Entry, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if e == nil {
		return errors.New("cache: nil entry")
	}
	if ttl <= 0 {
		ttl = c.defTTL
	}
	var expires time.Time
	if ttl > 0 {
		expires = Now().Add(ttl)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		en := el.Value.(*memEntry)
		en.value = e
		en.expiresAt = expires
		c.list.MoveToFront(el)
		c.stats.Writes++
		return nil
	}
	en := &memEntry{key: key, value: e, expiresAt: expires}
	el := c.list.PushFront(en)
	c.items[key] = el
	c.stats.Writes++
	for c.list.Len() > c.capacity {
		oldest := c.list.Back()
		if oldest == nil {
			break
		}
		c.removeLocked(oldest)
	}
	return nil
}

// Delete removes the entry. Missing key is not an error.
func (c *MemoryCache) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.removeLocked(el)
	}
	return nil
}

// Stats returns a snapshot.
func (c *MemoryCache) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// Close releases resources. The memory cache holds no external handles
// so this is a no-op kept for interface compliance.
func (c *MemoryCache) Close() error { return nil }

// Len returns the current entry count. Exposed for diagnostics/tests.
func (c *MemoryCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.list.Len()
}

func (c *MemoryCache) removeLocked(el *list.Element) {
	en := el.Value.(*memEntry)
	delete(c.items, en.key)
	c.list.Remove(el)
}
