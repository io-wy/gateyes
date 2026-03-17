package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/config"
)

type Cache struct {
	items   map[string]*CacheItem
	mu      sync.RWMutex
	maxSize int
	ttl     time.Duration
}

type CacheItem struct {
	Value     string
	ExpiresAt time.Time
}

func NewMemoryCache(cfg config.CacheConfig) *Cache {
	return &Cache{
		items:   make(map[string]*CacheItem),
		maxSize: cfg.MaxSize,
		ttl:     time.Duration(cfg.TTL) * time.Second,
	}
}

func (c *Cache) Get(prompt string) (string, bool) {
	key := c.hash(prompt)
	c.mu.RLock()
	defer c.mu.RUnlock()
	if item, ok := c.items[key]; ok {
		if time.Now().Before(item.ExpiresAt) {
			return item.Value, true
		}
	}
	return "", false
}

func (c *Cache) Set(prompt, response string) {
	key := c.hash(prompt)
	c.mu.Lock()
	defer c.mu.Unlock()

	// LRU: if full, delete oldest
	if len(c.items) >= c.maxSize {
		c.evictOldest()
	}

	c.items[key] = &CacheItem{
		Value:     response,
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

func (c *Cache) hash(prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(h[:])
}

func (c *Cache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	now := time.Now()
	for k, v := range c.items {
		if oldestTime.IsZero() || v.ExpiresAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.ExpiresAt
		}
	}
	if oldestKey != "" && now.After(oldestTime) {
		delete(c.items, oldestKey)
	}
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*CacheItem)
}

func (c *Cache) Stats() (hit, miss int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return 0, len(c.items)
}
