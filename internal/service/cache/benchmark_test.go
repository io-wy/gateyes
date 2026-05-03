package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func BenchmarkMemoryCache_Get(b *testing.B) {
	c := NewMemoryCache(MemoryConfig{Capacity: 1024})
	ctx := context.Background()
	_ = c.Set(ctx, "key", newTestEntry("m"), 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = c.Get(ctx, "key")
	}
}

func BenchmarkMemoryCache_Get_Parallel(b *testing.B) {
	c := NewMemoryCache(MemoryConfig{Capacity: 1024})
	ctx := context.Background()
	_ = c.Set(ctx, "key", newTestEntry("m"), 0)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = c.Get(ctx, "key")
		}
	})
}

func BenchmarkMemoryCache_Set(b *testing.B) {
	c := NewMemoryCache(MemoryConfig{Capacity: 1024})
	ctx := context.Background()
	e := newTestEntry("m")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Set(ctx, "key", e, 0)
	}
}

func BenchmarkMemoryCache_Set_Parallel(b *testing.B) {
	c := NewMemoryCache(MemoryConfig{Capacity: 1024})
	ctx := context.Background()
	e := newTestEntry("m")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = c.Set(ctx, "key", e, 0)
		}
	})
}

func BenchmarkMemoryCache_SetGetDelete(b *testing.B) {
	c := NewMemoryCache(MemoryConfig{Capacity: 1024})
	ctx := context.Background()
	e := newTestEntry("m")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Set(ctx, "key", e, 0)
		_, _, _ = c.Get(ctx, "key")
		_ = c.Delete(ctx, "key")
	}
}

func BenchmarkBuildKey(b *testing.B) {
	in := KeyInput{
		TenantID:    "tenant-abc",
		Model:       "gpt-4o-mini",
		PromptCanon: `{"messages":[{"role":"user","content":"hello world"}]}`,
		Stream:      false,
		Surface:     "chat_completions",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = BuildKey(in)
	}
}

func BenchmarkCanonicalizeJSON(b *testing.B) {
	payload := map[string]any{
		"model": "gpt-4o-mini",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
			map[string]any{"role": "assistant", "content": "hi there"},
		},
		"temperature": 0.7,
		"stream":      false,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CanonicalizeJSON(payload)
	}
}

func BenchmarkRedisCache_Get(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	c := NewRedisCache(rdb, RedisConfig{})
	ctx := context.Background()
	_ = c.Set(ctx, "key", newTestEntry("m"), 0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = c.Get(ctx, "key")
	}
}

func BenchmarkRedisCache_Get_Parallel(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	c := NewRedisCache(rdb, RedisConfig{})
	ctx := context.Background()
	_ = c.Set(ctx, "key", newTestEntry("m"), 0)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = c.Get(ctx, "key")
		}
	})
}

func BenchmarkRedisCache_Set(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	c := NewRedisCache(rdb, RedisConfig{})
	ctx := context.Background()
	e := newTestEntry("m")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Set(ctx, "key", e, time.Hour)
	}
}

func BenchmarkRedisCache_Set_Parallel(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	c := NewRedisCache(rdb, RedisConfig{})
	ctx := context.Background()
	e := newTestEntry("m")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = c.Set(ctx, "key", e, time.Hour)
		}
	})
}
