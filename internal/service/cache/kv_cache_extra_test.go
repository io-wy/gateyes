package cache

import (
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
)

func TestCacheCleanupExpiredRemovesExpiredItems(t *testing.T) {
	c := NewMemoryCache(config.CacheConfig{Enabled: true, MaxSize: 8, TTL: 60})
	c.Set("prompt-1", "response-1")

	key := c.hash("prompt-1")
	c.mu.Lock()
	c.items[key].ExpiresAt = time.Now().Add(-time.Second)
	c.mu.Unlock()

	c.cleanupExpired()

	if _, ok := c.Get("prompt-1"); ok {
		t.Fatal("cleanupExpired() kept expired item, want miss")
	}
}
