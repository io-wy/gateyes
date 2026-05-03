package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupRedisCache(t *testing.T, cfg RedisConfig) (*RedisCache, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	rc := NewRedisCache(rdb, cfg)
	t.Cleanup(func() {
		_ = rdb.Close()
		mr.Close()
	})
	return rc, mr, rdb
}

func TestRedisCache_SetGet(t *testing.T) {
	rc, _, _ := setupRedisCache(t, RedisConfig{})
	ctx := context.Background()
	want := newTestEntry("m")
	if err := rc.Set(ctx, "k1", want, 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, hit, err := rc.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !hit {
		t.Fatalf("expected hit")
	}
	if got.Model != "m" || got.Provider != "p1" || got.Usage.TotalTokens != 10 {
		t.Fatalf("entry round-trip mismatch: %+v", got)
	}
}

func TestRedisCache_Miss(t *testing.T) {
	rc, _, _ := setupRedisCache(t, RedisConfig{})
	got, hit, err := rc.Get(context.Background(), "absent")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if hit || got != nil {
		t.Fatalf("expected miss")
	}
	s := rc.Stats()
	if s.Misses != 1 {
		t.Fatalf("expected 1 miss, got %+v", s)
	}
}

func TestRedisCache_TTLExpiry(t *testing.T) {
	rc, mr, _ := setupRedisCache(t, RedisConfig{})
	ctx := context.Background()
	if err := rc.Set(ctx, "k", newTestEntry("m"), 100*time.Millisecond); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, hit, _ := rc.Get(ctx, "k"); !hit {
		t.Fatalf("expected hit before expiry")
	}
	mr.FastForward(200 * time.Millisecond)
	if _, hit, _ := rc.Get(ctx, "k"); hit {
		t.Fatalf("expected miss after expiry")
	}
}

func TestRedisCache_DefaultTTL(t *testing.T) {
	rc, mr, _ := setupRedisCache(t, RedisConfig{DefaultTTL: 50 * time.Millisecond})
	ctx := context.Background()
	if err := rc.Set(ctx, "k", newTestEntry("m"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	mr.FastForward(100 * time.Millisecond)
	if _, hit, _ := rc.Get(ctx, "k"); hit {
		t.Fatalf("expected default TTL to expire")
	}
}

func TestRedisCache_NoExpiryWhenZero(t *testing.T) {
	rc, mr, _ := setupRedisCache(t, RedisConfig{}) // DefaultTTL=0 ⇒ no expiry
	ctx := context.Background()
	if err := rc.Set(ctx, "k", newTestEntry("m"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	mr.FastForward(time.Hour)
	if _, hit, _ := rc.Get(ctx, "k"); !hit {
		t.Fatalf("expected entry to persist when no TTL configured")
	}
}

func TestRedisCache_Delete(t *testing.T) {
	rc, _, _ := setupRedisCache(t, RedisConfig{})
	ctx := context.Background()
	_ = rc.Set(ctx, "k", newTestEntry("m"), 0)
	if err := rc.Delete(ctx, "k"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, hit, _ := rc.Get(ctx, "k"); hit {
		t.Fatalf("expected miss after delete")
	}
	if err := rc.Delete(ctx, "absent"); err != nil {
		t.Fatalf("delete absent should not error, got %v", err)
	}
}

func TestRedisCache_Stats(t *testing.T) {
	rc, _, _ := setupRedisCache(t, RedisConfig{})
	ctx := context.Background()
	_, _, _ = rc.Get(ctx, "miss1")
	_ = rc.Set(ctx, "k", newTestEntry("m"), 0)
	_, _, _ = rc.Get(ctx, "k")
	_, _, _ = rc.Get(ctx, "k")
	s := rc.Stats()
	if s.Misses != 1 || s.Hits != 2 || s.Writes != 1 {
		t.Fatalf("unexpected stats %+v", s)
	}
}

func TestRedisCache_NilEntryRejected(t *testing.T) {
	rc, _, _ := setupRedisCache(t, RedisConfig{})
	if err := rc.Set(context.Background(), "k", nil, 0); err == nil {
		t.Fatalf("expected error for nil entry")
	}
}

func TestRedisCache_ContextCanceled(t *testing.T) {
	rc, _, _ := setupRedisCache(t, RedisConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := rc.Get(ctx, "k"); err == nil {
		t.Fatalf("expected ctx err on Get")
	}
	if err := rc.Set(ctx, "k", newTestEntry("m"), 0); err == nil {
		t.Fatalf("expected ctx err on Set")
	}
	if err := rc.Delete(ctx, "k"); err == nil {
		t.Fatalf("expected ctx err on Delete")
	}
}

func TestRedisCache_BackendDownReturnsError(t *testing.T) {
	rc, mr, _ := setupRedisCache(t, RedisConfig{})
	ctx := context.Background()
	if err := rc.Set(ctx, "k", newTestEntry("m"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
	mr.Close() // simulate backend outage
	if _, _, err := rc.Get(ctx, "k"); err == nil {
		t.Fatalf("expected error when backend is down")
	}
	if err := rc.Set(ctx, "k", newTestEntry("m"), 0); err == nil {
		t.Fatalf("expected error when backend is down")
	}
	s := rc.Stats()
	if s.Errors == 0 {
		t.Fatalf("expected error counter to increment, got %+v", s)
	}
}

func TestRedisCache_NilClientGuards(t *testing.T) {
	rc := NewRedisCache(nil, RedisConfig{})
	ctx := context.Background()
	if _, _, err := rc.Get(ctx, "k"); err == nil {
		t.Fatalf("expected error with nil client on Get")
	}
	if err := rc.Set(ctx, "k", newTestEntry("m"), 0); err == nil {
		t.Fatalf("expected error with nil client on Set")
	}
	if err := rc.Delete(ctx, "k"); err == nil {
		t.Fatalf("expected error with nil client on Delete")
	}
}

func TestRedisCache_Close(t *testing.T) {
	rc, _, _ := setupRedisCache(t, RedisConfig{})
	if err := rc.Close(); err != nil {
		t.Fatalf("close should be no-op, got %v", err)
	}
}
