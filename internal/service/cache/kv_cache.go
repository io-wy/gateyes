package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/config"
)

// CacheItem 缓存项
type CacheItem struct {
	Value      string
	ExpiresAt  time.Time
	AccessTime time.Time // 用于 LRU 淘汰
	HitCount   int64     // 命中次数统计
}

// Cache LRU 缓存
type Cache struct {
	items      map[string]*CacheItem
	mu         sync.RWMutex
	maxSize    int
	ttl        time.Duration

	// 统计
	hitCount    int64
	missCount   int64
	evictCount  int64

	// LRU 访问顺序
	accessOrder []string
}

// CacheStats 缓存统计
type CacheStats struct {
	MaxSize    int     `json:"max_size"`
	CurrentSize int    `json:"current_size"`
	HitCount   int64   `json:"hit_count"`
	MissCount  int64   `json:"miss_count"`
	HitRate    float64 `json:"hit_rate"`
	EvictCount int64   `json:"evict_count"`
}

func NewMemoryCache(cfg config.CacheConfig) *Cache {
	c := &Cache{
		items:      make(map[string]*CacheItem),
		maxSize:    cfg.MaxSize,
		ttl:        time.Duration(cfg.TTL) * time.Second,
		accessOrder: make([]string, 0, cfg.MaxSize),
	}
	// 启动过期清理 goroutine
	go c.cleanupLoop()
	return c
}

func (c *Cache) Get(prompt string) (string, bool) {
	key := c.hash(prompt)
	c.mu.Lock()
	defer c.mu.Unlock()

	if item, ok := c.items[key]; ok {
		if time.Now().Before(item.ExpiresAt) {
			// 命中，更新访问时间
			item.AccessTime = time.Now()
			item.HitCount++
			c.hitCount++
			// 移动到 accessOrder 末尾（LRU）
			c.moveToEnd(key)
			return item.Value, true
		}
		// 已过期，删除
		delete(c.items, key)
		c.removeFromOrder(key)
	}
	c.missCount++
	return "", false
}

func (c *Cache) Set(prompt, response string) {
	key := c.hash(prompt)
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果 key 已存在，更新值
	if item, ok := c.items[key]; ok {
		item.Value = response
		item.ExpiresAt = time.Now().Add(c.ttl)
		item.AccessTime = time.Now()
		c.moveToEnd(key)
		return
	}

	// LRU: 如果满了，删除最旧的
	if len(c.items) >= c.maxSize {
		c.evictOldest()
	}

	// 添加新项
	c.items[key] = &CacheItem{
		Value:      response,
		ExpiresAt:  time.Now().Add(c.ttl),
		AccessTime: time.Now(),
		HitCount:   0,
	}
	c.accessOrder = append(c.accessOrder, key)
}

func (c *Cache) hash(prompt string) string {
	h := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(h[:])
}

// evictOldest 淘汰最旧的项（LRU）
func (c *Cache) evictOldest() {
	if len(c.accessOrder) == 0 {
		return
	}
	// 淘汰最旧的（accessOrder 第一个）
	oldestKey := c.accessOrder[0]
	delete(c.items, oldestKey)
	c.accessOrder = c.accessOrder[1:]
	c.evictCount++
}

// cleanupLoop 定期清理过期项
func (c *Cache) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanupExpired()
	}
}

// cleanupExpired 清理过期项
func (c *Cache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	keysToDelete := make([]string, 0)

	for key, item := range c.items {
		if now.After(item.ExpiresAt) {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		delete(c.items, key)
		c.removeFromOrder(key)
	}
}

// moveToEnd 将 key 移动到 accessOrder 末尾
func (c *Cache) moveToEnd(key string) {
	for i, k := range c.accessOrder {
		if k == key {
			c.accessOrder = append(c.accessOrder[:i], c.accessOrder[i+1:]...)
			c.accessOrder = append(c.accessOrder, key)
			return
		}
	}
}

// removeFromOrder 从访问顺序中移除
func (c *Cache) removeFromOrder(key string) {
	for i, k := range c.accessOrder {
		if k == key {
			c.accessOrder = append(c.accessOrder[:i], c.accessOrder[i+1:]...)
			return
		}
	}
}

func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*CacheItem)
	c.accessOrder = make([]string, 0, c.maxSize)
}

// Stats 返回缓存统计
func (c *Cache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hitCount + c.missCount
	hitRate := 0.0
	if total > 0 {
		hitRate = float64(c.hitCount) / float64(total) * 100
	}

	return CacheStats{
		MaxSize:    c.maxSize,
		CurrentSize: len(c.items),
		HitCount:   c.hitCount,
		MissCount:  c.missCount,
		HitRate:    hitRate,
		EvictCount: c.evictCount,
	}
}
