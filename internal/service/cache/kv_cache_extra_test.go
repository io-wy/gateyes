package cache

import (
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
)

func TestCacheEvictionByTTL(t *testing.T) {
	c := NewMemoryCache(config.CacheConfig{Enabled: true, MaxSize: 100, TTL: 1})
	c.Set("key1", "value1")

	// 等待过期
	time.Sleep(2 * time.Second)

	// 手动触发清理
	c.cleanupExpired()

	stats := c.Stats()
	if stats.EvictedByTTL != 1 {
		t.Fatalf("EvictedByTTL = %d, want 1", stats.EvictedByTTL)
	}
}

func TestCacheEvictionByLRU(t *testing.T) {
	c := NewMemoryCache(config.CacheConfig{Enabled: true, MaxSize: 2, TTL: 3600})
	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3") // 应该淘汰 key1

	stats := c.Stats()
	if stats.EvictedByLRU != 1 {
		t.Fatalf("EvictedByLRU = %d, want 1", stats.EvictedByLRU)
	}
}

func TestCacheStats(t *testing.T) {
	c := NewMemoryCache(config.CacheConfig{Enabled: true, MaxSize: 10, TTL: 3600})

	// 未命中
	c.Get("nonexistent")

	// 命中
	c.Set("key1", "value1")
	c.Get("key1")

	// 再次命中
	c.Get("key1")

	stats := c.Stats()

	if stats.CurrentSize != 1 {
		t.Fatalf("CurrentSize = %d, want 1", stats.CurrentSize)
	}
	if stats.HitCount != 2 {
		t.Fatalf("HitCount = %d, want 2", stats.HitCount)
	}
	if stats.MissCount != 1 {
		t.Fatalf("MissCount = %d, want 1", stats.MissCount)
	}
	// Hit rate = 2/3 * 100 = 66.67%
	if stats.HitRate < 66 || stats.HitRate > 67 {
		t.Fatalf("HitRate = %f, want ~66.67", stats.HitRate)
	}
}
