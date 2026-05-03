package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestEntry(model string) *Entry {
	return &Entry{
		Response:  []byte(`{"id":"r-1"}`),
		Model:     model,
		Provider:  "p1",
		Usage:     Usage{TotalTokens: 10},
		CreatedAt: 1700000000,
	}
}

func TestMemoryCache_SetGet(t *testing.T) {
	c := NewMemoryCache(MemoryConfig{Capacity: 4})
	ctx := context.Background()
	if err := c.Set(ctx, "k1", newTestEntry("m"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, hit, err := c.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !hit {
		t.Fatalf("expected hit")
	}
	if got.Model != "m" {
		t.Fatalf("expected m, got %s", got.Model)
	}
}

func TestMemoryCache_Miss(t *testing.T) {
	c := NewMemoryCache(MemoryConfig{Capacity: 4})
	got, hit, err := c.Get(context.Background(), "absent")
	if err != nil {
		t.Fatalf("get err: %v", err)
	}
	if hit || got != nil {
		t.Fatalf("expected miss")
	}
}

func TestMemoryCache_TTLExpiry(t *testing.T) {
	frozen := time.Unix(1700000000, 0)
	prev := Now
	Now = func() time.Time { return frozen }
	defer func() { Now = prev }()

	c := NewMemoryCache(MemoryConfig{Capacity: 4})
	ctx := context.Background()
	if err := c.Set(ctx, "k", newTestEntry("m"), 100*time.Millisecond); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, hit, _ := c.Get(ctx, "k"); !hit {
		t.Fatalf("expected hit before expiry")
	}
	Now = func() time.Time { return frozen.Add(200 * time.Millisecond) }
	if _, hit, _ := c.Get(ctx, "k"); hit {
		t.Fatalf("expected miss after expiry")
	}
}

func TestMemoryCache_LRUEviction(t *testing.T) {
	c := NewMemoryCache(MemoryConfig{Capacity: 2})
	ctx := context.Background()
	_ = c.Set(ctx, "a", newTestEntry("a"), 0)
	_ = c.Set(ctx, "b", newTestEntry("b"), 0)
	// touch a so b becomes LRU
	if _, hit, _ := c.Get(ctx, "a"); !hit {
		t.Fatalf("a should be present")
	}
	_ = c.Set(ctx, "c", newTestEntry("c"), 0)
	if _, hit, _ := c.Get(ctx, "b"); hit {
		t.Fatalf("b should have been evicted")
	}
	if _, hit, _ := c.Get(ctx, "a"); !hit {
		t.Fatalf("a should still be present")
	}
	if _, hit, _ := c.Get(ctx, "c"); !hit {
		t.Fatalf("c should be present")
	}
	if got := c.Len(); got != 2 {
		t.Fatalf("expected len 2, got %d", got)
	}
}

func TestMemoryCache_UpdateExisting(t *testing.T) {
	c := NewMemoryCache(MemoryConfig{Capacity: 4})
	ctx := context.Background()
	_ = c.Set(ctx, "k", newTestEntry("v1"), 0)
	_ = c.Set(ctx, "k", newTestEntry("v2"), 0)
	got, _, _ := c.Get(ctx, "k")
	if got.Model != "v2" {
		t.Fatalf("expected updated value v2, got %s", got.Model)
	}
	if got := c.Len(); got != 1 {
		t.Fatalf("expected len 1, got %d", got)
	}
}

func TestMemoryCache_Delete(t *testing.T) {
	c := NewMemoryCache(MemoryConfig{Capacity: 4})
	ctx := context.Background()
	_ = c.Set(ctx, "k", newTestEntry("m"), 0)
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, hit, _ := c.Get(ctx, "k"); hit {
		t.Fatalf("expected miss after delete")
	}
	// deleting absent key is OK
	if err := c.Delete(ctx, "absent"); err != nil {
		t.Fatalf("delete absent should not error, got %v", err)
	}
}

func TestMemoryCache_Stats(t *testing.T) {
	c := NewMemoryCache(MemoryConfig{Capacity: 4})
	ctx := context.Background()
	_, _, _ = c.Get(ctx, "miss1")
	_ = c.Set(ctx, "k", newTestEntry("m"), 0)
	_, _, _ = c.Get(ctx, "k")
	_, _, _ = c.Get(ctx, "k")
	s := c.Stats()
	if s.Misses != 1 || s.Hits != 2 || s.Writes != 1 {
		t.Fatalf("unexpected stats %+v", s)
	}
}

func TestMemoryCache_NilEntryRejected(t *testing.T) {
	c := NewMemoryCache(MemoryConfig{Capacity: 4})
	if err := c.Set(context.Background(), "k", nil, 0); err == nil {
		t.Fatalf("expected error for nil entry")
	}
}

func TestMemoryCache_DefaultCapacity(t *testing.T) {
	c := NewMemoryCache(MemoryConfig{Capacity: 0})
	if c.capacity != 1024 {
		t.Fatalf("expected default capacity 1024, got %d", c.capacity)
	}
}

func TestMemoryCache_ContextCanceled(t *testing.T) {
	c := NewMemoryCache(MemoryConfig{Capacity: 4})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.Get(ctx, "k"); err == nil {
		t.Fatalf("expected ctx.Err on cancel")
	}
	if err := c.Set(ctx, "k", newTestEntry("m"), 0); err == nil {
		t.Fatalf("expected ctx.Err on cancel")
	}
	if err := c.Delete(ctx, "k"); err == nil {
		t.Fatalf("expected ctx.Err on cancel")
	}
}

func TestMemoryCache_ConcurrentAccess(t *testing.T) {
	c := NewMemoryCache(MemoryConfig{Capacity: 256})
	var wg sync.WaitGroup
	var sets, gets atomic.Int64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			ctx := context.Background()
			for j := 0; j < 200; j++ {
				key := []byte{byte(seed), byte(j)}
				_ = c.Set(ctx, string(key), newTestEntry("m"), 0)
				sets.Add(1)
				_, _, _ = c.Get(ctx, string(key))
				gets.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if sets.Load() != 1600 || gets.Load() != 1600 {
		t.Fatalf("expected 1600 ops each, got sets=%d gets=%d", sets.Load(), gets.Load())
	}
}

func TestMemoryCache_DefaultTTL(t *testing.T) {
	frozen := time.Unix(1700000000, 0)
	prev := Now
	Now = func() time.Time { return frozen }
	defer func() { Now = prev }()
	c := NewMemoryCache(MemoryConfig{Capacity: 4, DefaultTTL: 50 * time.Millisecond})
	ctx := context.Background()
	_ = c.Set(ctx, "k", newTestEntry("m"), 0)
	Now = func() time.Time { return frozen.Add(100 * time.Millisecond) }
	if _, hit, _ := c.Get(ctx, "k"); hit {
		t.Fatalf("default TTL should expire")
	}
}
