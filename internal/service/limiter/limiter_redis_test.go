package limiter

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/gateyes/gateway/internal/config"
)

func setupRedisLimiter(t *testing.T) (*Limiter, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	cfg := config.LimiterConfig{
		GlobalQPS:           10000,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000,
		GlobalRPM:           600,
		GlobalRPMBurst:      60,
		PerUserRequestBurst: 100,
		TenantTPM:           100,
		TenantTPMBurst:      10,
		TenantRPM:           60,
		TenantRPMBurst:      5,
		ProviderTPM:         100,
		ProviderTPMBurst:    10,
		ProviderRPM:         60,
		ProviderRPMBurst:    5,
		ModelTPM:            100,
		ModelTPMBurst:       10,
		ModelRPM:            60,
		ModelRPMBurst:       5,
		QueueSize:           1000,
	}
	l := NewLimiter(cfg)
	l.SetRedis(rdb)
	t.Cleanup(func() {
		l.Stop()
		rdb.Close()
		mr.Close()
	})
	return l, mr
}

func TestLimiter_RedisCheckTenant(t *testing.T) {
	l, _ := setupRedisLimiter(t)

	if !l.CheckTenant("tenant-a", 5) {
		t.Error("tenant should allow within burst")
	}
	for i := 0; i < 10; i++ {
		l.CheckTenant("tenant-a", 1)
	}
	if l.CheckTenant("tenant-a", 1) {
		t.Error("tenant should be rate limited after burst exhausted")
	}
	if !l.CheckTenant("tenant-b", 1) {
		t.Error("different tenant should not be affected")
	}
}

func TestLimiter_RedisCheckProvider(t *testing.T) {
	l, _ := setupRedisLimiter(t)

	if !l.CheckProvider("openai", 5) {
		t.Error("provider should allow within burst")
	}
	for i := 0; i < 10; i++ {
		l.CheckProvider("openai", 1)
	}
	if l.CheckProvider("openai", 1) {
		t.Error("provider should be rate limited after burst exhausted")
	}
	if !l.CheckProvider("anthropic", 1) {
		t.Error("different provider should not be affected")
	}
}

func TestLimiter_RedisCheckModel(t *testing.T) {
	l, _ := setupRedisLimiter(t)

	if !l.CheckModel("gpt-4", 5) {
		t.Error("model should allow within burst")
	}
	for i := 0; i < 10; i++ {
		l.CheckModel("gpt-4", 1)
	}
	if l.CheckModel("gpt-4", 1) {
		t.Error("model should be rate limited after burst exhausted")
	}
	if !l.CheckModel("claude", 1) {
		t.Error("different model should not be affected")
	}
}

func TestLimiter_RedisGlobalAllow(t *testing.T) {
	l, _ := setupRedisLimiter(t)
	ctx := context.Background()

	if !l.Allow(ctx, "user1", 0, 1) {
		t.Error("should allow within global limits")
	}
}

func TestLimiter_SetRedis(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:           100,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    100,
		PerUserRequestBurst: 100,
		QueueSize:           100,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	l.SetRedis(rdb)
	if l.rdb == nil {
		t.Error("SetRedis should set the Redis client")
	}
}

func TestLimiter_DisabledDimensionWithRedis(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cfg := config.LimiterConfig{
		GlobalQPS:           10000,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000,
		PerUserRequestBurst: 100,
		TenantTPM:           0,
		TenantRPM:           0,
		QueueSize:           1000,
	}
	l := NewLimiter(cfg)
	l.SetRedis(rdb)
	defer l.Stop()

	for i := 0; i < 1000; i++ {
		if !l.CheckTenant("tenant-x", 1) {
			t.Fatal("disabled tenant limit should always allow even with Redis")
		}
	}
}

func TestLimiter_RealRedis(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real Redis integration test in short mode")
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "dev_redis_pw_2026",
	})
	defer rdb.Close()

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	cfg := config.LimiterConfig{
		GlobalQPS:           10000,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000,
		TenantTPM:           100,
		TenantTPMBurst:      5,
		ProviderTPM:         100,
		ProviderTPMBurst:    5,
		PerUserRequestBurst: 100,
		QueueSize:           100,
	}
	l := NewLimiter(cfg)
	l.SetRedis(rdb)
	defer l.Stop()

	// Clean up keys from previous test runs
	rdb.Del(context.Background(), "gateyes:rl:ten:real-tenant:t", "gateyes:rl:ten:real-tenant:r",
		"gateyes:rl:prov:real-prov:t", "gateyes:rl:prov:real-prov:r")

	if !l.CheckTenant("real-tenant", 3) {
		t.Error("first tenant consume should succeed")
	}
	for i := 0; i < 5; i++ {
		l.CheckTenant("real-tenant", 1)
	}
	if l.CheckTenant("real-tenant", 1) {
		t.Error("tenant should be rate limited after burst")
	}

	if !l.CheckProvider("real-prov", 3) {
		t.Error("first provider consume should succeed")
	}

	if !l.Allow(context.Background(), "user1", 0, 1) {
		t.Error("global allow should succeed")
	}
}
