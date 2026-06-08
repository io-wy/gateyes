package limiter

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/gateyes/gateway/internal/app/config"
)

func BenchmarkTokenBucket_TryConsume(b *testing.B) {
	tb := NewTokenBucket(10000, 1000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.TryConsume(1)
	}
}

func BenchmarkTokenBucket_TryConsume_Parallel(b *testing.B) {
	tb := NewTokenBucket(10000, 1000)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			tb.TryConsume(1)
		}
	})
}

func BenchmarkLimiter_Allow(b *testing.B) {
	cfg := config.LimiterConfig{
		GlobalQPS:           10000,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000,
		GlobalRPM:           600,
		GlobalRPMBurst:      60,
		PerUserRequestBurst: 100,
		QueueSize:           1000,
	}

	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	l := NewLimiter(cfg)
	l.SetRedis(rdb)
	defer l.Stop()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = l.Allow(ctx, "user1", 0, 1)
	}
}

func BenchmarkLimiter_Allow_Parallel(b *testing.B) {
	cfg := config.LimiterConfig{
		GlobalQPS:           10000,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000,
		GlobalRPM:           600,
		GlobalRPMBurst:      60,
		PerUserRequestBurst: 100,
		QueueSize:           1000,
	}

	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	l := NewLimiter(cfg)
	l.SetRedis(rdb)
	defer l.Stop()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = l.Allow(ctx, "user1", 0, 1)
		}
	})
}

func BenchmarkLimiter_Allow_MemoryFallback(b *testing.B) {
	cfg := config.LimiterConfig{
		GlobalQPS:           10000,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000,
		GlobalRPM:           600,
		GlobalRPMBurst:      60,
		PerUserRequestBurst: 100,
		QueueSize:           1000,
	}

	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = l.Allow(ctx, "user1", 0, 1)
	}
}

func BenchmarkLimiter_Allow_MemoryFallback_Parallel(b *testing.B) {
	cfg := config.LimiterConfig{
		GlobalQPS:           10000,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000,
		GlobalRPM:           600,
		GlobalRPMBurst:      60,
		PerUserRequestBurst: 100,
		QueueSize:           1000,
	}

	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = l.Allow(ctx, "user1", 0, 1)
		}
	})
}
