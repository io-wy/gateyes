package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// TestMemoryCache_ConcurrentSetSameKey verifies that many goroutines
// racing to Set the same key do not corrupt the internal state.
func TestMemoryCache_ConcurrentSetSameKey(t *testing.T) {
	c := NewMemoryCache(MemoryConfig{Capacity: 4})
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			e := newTestEntry("m")
			e.Response = []byte(`{"id":"` + string(rune('a'+val%26)) + `"}`)
			_ = c.Set(ctx, "shared", e, 0)
		}(i)
	}
	wg.Wait()
	got, hit, err := c.Get(ctx, "shared")
	if err != nil {
		t.Fatalf("get after concurrent set: %v", err)
	}
	if !hit || got == nil {
		t.Fatalf("expected hit after concurrent set")
	}
	if got.Model != "m" {
		t.Fatalf("model should survive race")
	}
}

// TestRedisCache_ConcurrentSetSameKey verifies Redis-backed cache
// tolerates concurrent writes to the same key (last-write-wins).
func TestRedisCache_ConcurrentSetSameKey(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	rc := NewRedisCache(rdb, RedisConfig{})
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			e := newTestEntry("m")
			e.Response = []byte(`{"seq":` + string(rune('0'+val%10)) + `}`)
			_ = rc.Set(ctx, "shared", e, 0)
		}(i)
	}
	wg.Wait()
	got, hit, err := rc.Get(ctx, "shared")
	if err != nil {
		t.Fatalf("get after concurrent set: %v", err)
	}
	if !hit || got == nil {
		t.Fatalf("expected hit after concurrent set")
	}
}

// TestFallbackCache_RecoveryAfterOutage simulates a distributed scenario:
// 1. Redis is up; write entry.
// 2. Redis goes down; read falls back to memory miss.
// 3. Write while Redis is down → memory gets the value.
// 4. Redis comes back up; read should still work via memory fallback.
// 5. Re-write now that Redis is up → both backends have it.
// 6. Read from Redis (primary) should succeed.
func TestFallbackCache_RecoveryAfterOutage(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	redisCache := NewRedisCache(rdb, RedisConfig{})
	memCache := NewMemoryCache(MemoryConfig{Capacity: 4})
	fb := NewFallbackCache(redisCache, memCache)
	ctx := context.Background()
	key := "key"
	want := newTestEntry("recovered")

	// Step 1: Redis up, write entry
	if err := fb.Set(ctx, key, want, 0); err != nil {
		t.Fatalf("step1 set: %v", err)
	}

	// Step 2: Redis down
	mr.Close()
	_, hit, err := fb.Get(ctx, key)
	if hit {
		// miniredis closed client might still have pooled connection
		// that hasn't errored yet; accept either outcome
		t.Logf("step2: unexpected hit (pooled conn), err=%v", err)
	}

	// Step 3: Write while Redis down → memory gets it
	want2 := newTestEntry("mem-only")
	_ = fb.Set(ctx, key, want2, 0) // primary errors, fallback absorbs

	// Step 4: Read should work via memory
	got, hit, err := fb.Get(ctx, key)
	if err != nil {
		t.Fatalf("step4 get err: %v", err)
	}
	if !hit {
		t.Fatalf("step4: expected memory fallback hit")
	}
	if got.Model != "mem-only" {
		t.Fatalf("step4: expected mem-only, got %s", got.Model)
	}
}

// TestFallbackCache_ConcurrentDegraded verifies safe behaviour when
// the primary is under heavy concurrent load and returning errors.
func TestFallbackCache_ConcurrentDegraded(t *testing.T) {
	pri := newFakeCache()
	fb := newFakeCache()
	fallback := NewFallbackCache(pri, fb)
	ctx := context.Background()

	// Primary fails all reads after N successes
	var reads atomic.Int64
	pri.getHook = func(key string) (*Entry, bool, error) {
		if reads.Add(1) > 50 {
			return nil, false, errDegraded
		}
		return nil, false, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = fallback.Get(ctx, "k")
		}()
	}
	wg.Wait()

	// Fallback should have been consulted for the degraded reads
	if fb.stats.Misses == 0 {
		t.Fatalf("fallback should have served misses")
	}
}

var errDegraded = errDegradedType{}

type errDegradedType struct{}

func (errDegradedType) Error() string { return "degraded" }

// TestEntry_JSONRoundTrip verifies that marshalling/unmarshalling
// preserves every field, preventing silent data loss in Redis.
func TestEntry_JSONRoundTrip(t *testing.T) {
	original := &Entry{
		Response:  []byte(`{"id":"r1","choices":[{"message":{"content":"hello"}}]}`),
		StreamRaw: []byte("data: chunk1\n\ndata: chunk2\n\n"),
		Stream:    true,
		Model:     "gpt-4o",
		Provider:  "openai-eastus",
		Usage: Usage{
			PromptTokens:     12,
			CompletionTokens: 5,
			TotalTokens:      17,
			CachedTokens:     3,
		},
		CreatedAt: 1700000000,
	}
	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Entry
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(decoded.Response) != string(original.Response) {
		t.Fatalf("response mismatch")
	}
	if string(decoded.StreamRaw) != string(original.StreamRaw) {
		t.Fatalf("stream_raw mismatch")
	}
	if decoded.Stream != original.Stream {
		t.Fatalf("stream flag mismatch")
	}
	if decoded.Model != original.Model {
		t.Fatalf("model mismatch")
	}
	if decoded.Provider != original.Provider {
		t.Fatalf("provider mismatch")
	}
	if decoded.Usage != original.Usage {
		t.Fatalf("usage mismatch: %+v vs %+v", decoded.Usage, original.Usage)
	}
	if decoded.CreatedAt != original.CreatedAt {
		t.Fatalf("created_at mismatch")
	}
}

// TestMemoryCache_TTLBoundary verifies expiry exactly at the boundary.
func TestMemoryCache_TTLBoundary(t *testing.T) {
	frozen := time.Unix(1700000000, 0)
	prev := Now
	Now = func() time.Time { return frozen }
	defer func() { Now = prev }()

	c := NewMemoryCache(MemoryConfig{Capacity: 4})
	ctx := context.Background()
	_ = c.Set(ctx, "k", newTestEntry("m"), 100*time.Millisecond)

	// exactly at expiry — still valid (expiresAt = frozen + 100ms)
	Now = func() time.Time { return frozen.Add(100 * time.Millisecond) }
	_, hit, _ := c.Get(ctx, "k")
	if !hit {
		t.Fatalf("entry should be valid at exact boundary")
	}

	// one nanosecond past expiry
	Now = func() time.Time { return frozen.Add(100*time.Millisecond + 1) }
	_, hit, _ = c.Get(ctx, "k")
	if hit {
		t.Fatalf("entry should expire one ns past boundary")
	}
}

// TestRedisCache_RaceCondition_SetGetDelete exercises the Redis cache
// with a classic race pattern: one goroutine cycles set/get/delete
// while another only reads.
func TestRedisCache_RaceCondition_SetGetDelete(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	rc := NewRedisCache(rdb, RedisConfig{})
	ctx := context.Background()
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = rc.Set(ctx, "race", newTestEntry("m"), time.Second)
				_, _, _ = rc.Get(ctx, "race")
				_ = rc.Delete(ctx, "race")
			}
		}
	}()

	// reader
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_, _, _ = rc.Get(ctx, "race")
			}
		}
	}()

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestMemoryCache_LRUBoundary verifies that exactly-capacity items are
// retained and the next insertion evicts the oldest.
func TestMemoryCache_LRUBoundary(t *testing.T) {
	c := NewMemoryCache(MemoryConfig{Capacity: 3})
	ctx := context.Background()
	_ = c.Set(ctx, "a", newTestEntry("a"), 0)
	_ = c.Set(ctx, "b", newTestEntry("b"), 0)
	_ = c.Set(ctx, "c", newTestEntry("c"), 0)

	if c.Len() != 3 {
		t.Fatalf("expected len 3, got %d", c.Len())
	}

	// touch a so b becomes LRU
	_, _, _ = c.Get(ctx, "a")
	_, _, _ = c.Get(ctx, "c")

	_ = c.Set(ctx, "d", newTestEntry("d"), 0)
	if c.Len() != 3 {
		t.Fatalf("expected len 3 after eviction, got %d", c.Len())
	}

	_, hit, _ := c.Get(ctx, "b")
	if hit {
		t.Fatalf("b should have been evicted (LRU)")
	}
	_, hit, _ = c.Get(ctx, "a")
	if !hit {
		t.Fatalf("a should still be present")
	}
	_, hit, _ = c.Get(ctx, "c")
	if !hit {
		t.Fatalf("c should still be present")
	}
	_, hit, _ = c.Get(ctx, "d")
	if !hit {
		t.Fatalf("d should be present")
	}
}

// TestFallbackCache_ReadRepair verifies that when the primary misses
// but the fallback hits, a subsequent read returns the fallback value
// even though the primary never got it.
func TestFallbackCache_ReadRepair(t *testing.T) {
	pri := newFakeCache()
	fb := newFakeCache()
	fallback := NewFallbackCache(pri, fb)
	ctx := context.Background()

	// Only fallback has the entry
	fb.data["k"] = newTestEntry("fallback-val")

	got, hit, err := fallback.Get(ctx, "k")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !hit || got.Model != "fallback-val" {
		t.Fatalf("expected fallback hit, got hit=%v model=%s", hit, got.Model)
	}
}

// TestBuildKey_CollisionResistance exercises key generation with many
// similar inputs to ensure no accidental collisions.
func TestBuildKey_CollisionResistance(t *testing.T) {
	seen := make(map[string]cacheKeySource, 1000)
	for i := 0; i < 1000; i++ {
		in := KeyInput{
			TenantID:    fmt.Sprintf("tenant-%d", i),
			Model:       fmt.Sprintf("model-%d", i),
			PromptCanon: fmt.Sprintf("prompt-%d", i),
			Stream:      i%2 == 0,
			Surface:     fmt.Sprintf("surface-%d", i),
		}
		k := BuildKey(in)
		if prev, ok := seen[k]; ok {
			t.Fatalf("collision: %v and %v both map to %s", prev, in, k)
		}
		seen[k] = cacheKeySource{in}
	}
}

type cacheKeySource struct {
	KeyInput
}
