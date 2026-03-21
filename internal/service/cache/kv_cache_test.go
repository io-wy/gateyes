package cache

import (
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
)

func TestCache_SetAndGet(t *testing.T) {
	cfg := config.CacheConfig{
		Enabled: true,
		MaxSize: 100,
		TTL:     60,
	}
	c := NewMemoryCache(cfg)

	// 设置缓存
	c.Set("prompt1", "response1")

	// 获取缓存
	got, ok := c.Get("prompt1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got != "response1" {
		t.Fatalf("expected response1, got %s", got)
	}
}

func TestCache_Miss(t *testing.T) {
	cfg := config.CacheConfig{
		Enabled: true,
		MaxSize: 100,
		TTL:     60,
	}
	c := NewMemoryCache(cfg)

	// 不存在的 key
	got, ok := c.Get("nonexistent")
	if ok {
		t.Fatalf("expected cache miss, got %s", got)
	}
}

func TestCache_LRU_Eviction(t *testing.T) {
	cfg := config.CacheConfig{
		Enabled: true,
		MaxSize: 3,
		TTL:     60,
	}
	c := NewMemoryCache(cfg)

	// 添加 3 项
	c.Set("prompt1", "response1")
	c.Set("prompt2", "response2")
	c.Set("prompt3", "response3")

	// 访问 prompt1（更新为最新）
	c.Get("prompt1")

	// 添加第 4 项，应该淘汰 prompt2（最旧未访问的）
	c.Set("prompt4", "response4")

	// prompt1 仍然存在
	if _, ok := c.Get("prompt1"); !ok {
		t.Error("prompt1 should still exist after LRU update")
	}

	// prompt2 应该被淘汰
	if _, ok := c.Get("prompt2"); ok {
		t.Error("prompt2 should be evicted (LRU)")
	}

	// prompt3 仍然存在
	if _, ok := c.Get("prompt3"); !ok {
		t.Error("prompt3 should still exist")
	}
}

func TestCache_Expiration(t *testing.T) {
	cfg := config.CacheConfig{
		Enabled: true,
		MaxSize: 100,
		TTL:     1, // 1秒
	}
	c := NewMemoryCache(cfg)

	c.Set("prompt1", "response1")

	// 立即获取，应该命中
	if _, ok := c.Get("prompt1"); !ok {
		t.Error("should hit immediately")
	}

	// 等待过期
	time.Sleep(1100 * time.Millisecond)

	// 应该过期
	if _, ok := c.Get("prompt1"); ok {
		t.Error("should be expired")
	}
}

func TestCache_Stats(t *testing.T) {
	cfg := config.CacheConfig{
		Enabled: true,
		MaxSize: 100,
		TTL:     60,
	}
	c := NewMemoryCache(cfg)

	// 初始统计
	stats := c.Stats()
	if stats.HitCount != 0 {
		t.Fatalf("initial hit count should be 0, got %d", stats.HitCount)
	}
	if stats.MissCount != 0 {
		t.Fatalf("initial miss count should be 0, got %d", stats.MissCount)
	}

	// 一次命中
	c.Set("prompt1", "response1")
	c.Get("prompt1")

	// 一次未命中
	c.Get("nonexistent")

	stats = c.Stats()
	if stats.HitCount != 1 {
		t.Fatalf("hit count should be 1, got %d", stats.HitCount)
	}
	if stats.MissCount != 1 {
		t.Fatalf("miss count should be 1, got %d", stats.MissCount)
	}

	// 验证命中率
	expectedHitRate := 50.0
	if stats.HitRate != expectedHitRate {
		t.Fatalf("hit rate should be %.2f, got %.2f", expectedHitRate, stats.HitRate)
	}
}

func TestCache_UpdateExisting(t *testing.T) {
	cfg := config.CacheConfig{
		Enabled: true,
		MaxSize: 100,
		TTL:     60,
	}
	c := NewMemoryCache(cfg)

	// 设置初始值
	c.Set("prompt1", "response1")

	// 更新值
	c.Set("prompt1", "response2")

	got, ok := c.Get("prompt1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got != "response2" {
		t.Fatalf("expected updated response2, got %s", got)
	}

	// 验证只有一项
	stats := c.Stats()
	if stats.CurrentSize != 1 {
		t.Fatalf("expected 1 item, got %d", stats.CurrentSize)
	}
}

func TestCache_Clear(t *testing.T) {
	cfg := config.CacheConfig{
		Enabled: true,
		MaxSize: 100,
		TTL:     60,
	}
	c := NewMemoryCache(cfg)

	c.Set("prompt1", "response1")
	c.Set("prompt2", "response2")

	c.Clear()

	stats := c.Stats()
	if stats.CurrentSize != 0 {
		t.Fatalf("expected 0 items after clear, got %d", stats.CurrentSize)
	}
	if _, ok := c.Get("prompt1"); ok {
		t.Error("prompt1 should not exist after clear")
	}
}

func TestCache_Concurrent(t *testing.T) {
	cfg := config.CacheConfig{
		Enabled: true,
		MaxSize: 1000,
		TTL:     60,
	}
	c := NewMemoryCache(cfg)

	done := make(chan bool)

	// 并发写入
	go func() {
		for i := 0; i < 100; i++ {
			c.Set("prompt", "response")
		}
		done <- true
	}()

	// 并发读取
	go func() {
		for i := 0; i < 100; i++ {
			c.Get("prompt")
		}
		done <- true
	}()

	<-done
	<-done

	// 不应该 panic 或崩溃
	stats := c.Stats()
	if stats.CurrentSize > 1 {
		t.Logf("concurrent test passed, size: %d", stats.CurrentSize)
	}
}
