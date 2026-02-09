package cache

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// MemoryCache implements an in-memory LRU cache
type MemoryCache struct {
	maxSize    int64
	maxEntries int
	entries    map[string]*list.Element
	lru        *list.List
	size       int64
	stats      CacheStats
	mu         sync.RWMutex
}

// cacheItem represents an item in the cache
type cacheItem struct {
	key       string
	value     []byte
	expiresAt time.Time
	size      int64
}

// NewMemoryCache creates a new in-memory cache
func NewMemoryCache(maxSize int64, maxEntries int) *MemoryCache {
	if maxSize <= 0 {
		maxSize = 100 * 1024 * 1024 // 100MB default
	}
	if maxEntries <= 0 {
		maxEntries = 10000 // 10k entries default
	}

	return &MemoryCache{
		maxSize:    maxSize,
		maxEntries: maxEntries,
		entries:    make(map[string]*list.Element),
		lru:        list.New(),
	}
}

// Get retrieves a value from cache
func (mc *MemoryCache) Get(ctx context.Context, key string) ([]byte, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	elem, exists := mc.entries[key]
	if !exists {
		mc.stats.Misses++
		return nil, ErrCacheMiss
	}

	item := elem.Value.(*cacheItem)

	// Check expiration
	if time.Now().After(item.expiresAt) {
		mc.removeElement(elem)
		mc.stats.Misses++
		return nil, ErrExpired
	}

	// Move to front (most recently used)
	mc.lru.MoveToFront(elem)
	mc.stats.Hits++

	return item.value, nil
}

// Set stores a value in cache
func (mc *MemoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	itemSize := int64(len(key) + len(value))

	// Check if item already exists
	if elem, exists := mc.entries[key]; exists {
		// Update existing item
		item := elem.Value.(*cacheItem)
		mc.size -= item.size
		item.value = value
		item.expiresAt = time.Now().Add(ttl)
		item.size = itemSize
		mc.size += itemSize
		mc.lru.MoveToFront(elem)
		mc.stats.Sets++
		return nil
	}

	// Evict items if necessary
	for mc.size+itemSize > mc.maxSize || mc.lru.Len() >= mc.maxEntries {
		if mc.lru.Len() == 0 {
			return ErrCacheFull
		}
		mc.removeOldest()
	}

	// Add new item
	item := &cacheItem{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(ttl),
		size:      itemSize,
	}

	elem := mc.lru.PushFront(item)
	mc.entries[key] = elem
	mc.size += itemSize
	mc.stats.Sets++

	return nil
}

// Delete removes a value from cache
func (mc *MemoryCache) Delete(ctx context.Context, key string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	elem, exists := mc.entries[key]
	if !exists {
		return nil
	}

	mc.removeElement(elem)
	mc.stats.Deletes++
	return nil
}

// Clear removes all values from cache
func (mc *MemoryCache) Clear(ctx context.Context) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.entries = make(map[string]*list.Element)
	mc.lru = list.New()
	mc.size = 0

	return nil
}

// Stats returns cache statistics
func (mc *MemoryCache) Stats() CacheStats {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	stats := mc.stats
	stats.Size = mc.size
	return stats
}

// removeOldest removes the oldest item from cache
func (mc *MemoryCache) removeOldest() {
	elem := mc.lru.Back()
	if elem != nil {
		mc.removeElement(elem)
		mc.stats.Evictions++
	}
}

// removeElement removes an element from cache
func (mc *MemoryCache) removeElement(elem *list.Element) {
	item := elem.Value.(*cacheItem)
	delete(mc.entries, item.key)
	mc.lru.Remove(elem)
	mc.size -= item.size
}

// CleanExpired removes expired entries (can be called periodically)
func (mc *MemoryCache) CleanExpired() int {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	now := time.Now()
	removed := 0

	// Iterate through all entries
	for _, elem := range mc.entries {
		item := elem.Value.(*cacheItem)
		if now.After(item.expiresAt) {
			mc.removeElement(elem)
			removed++
		}
	}

	return removed
}

// StartCleanupRoutine starts a background goroutine to clean expired entries
func (mc *MemoryCache) StartCleanupRoutine(interval time.Duration) chan struct{} {
	stopChan := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				removed := mc.CleanExpired()
				if removed > 0 {
					// Log cleanup activity
					_ = removed
				}
			case <-stopChan:
				return
			}
		}
	}()

	return stopChan
}
