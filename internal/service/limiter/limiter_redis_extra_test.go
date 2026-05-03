package limiter

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/gateyes/gateway/internal/config"
)

func TestLimiter_RedisModelAllow(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cfg := config.LimiterConfig{
		GlobalQPS:        10000,
		GlobalTPM:        1000000,
		GlobalTokenBurst: 1000,
		ModelTPM:         100,
		ModelTPMBurst:    10,
		ModelRPM:         60,
		ModelRPMBurst:    5,
		QueueSize:        100,
	}
	l := NewLimiter(cfg)
	l.SetRedis(rdb)
	defer l.Stop()

	if !l.CheckModel("gpt-4", 5) {
		t.Error("Redis model should allow within burst")
	}
	for i := 0; i < 10; i++ {
		l.CheckModel("gpt-4", 1)
	}
	if l.CheckModel("gpt-4", 1) {
		t.Error("Redis model should reject after burst exhausted")
	}
	if !l.CheckModel("claude", 1) {
		t.Error("different model should not be affected")
	}
}

func TestLimiter_RedisTenantAllow(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cfg := config.LimiterConfig{
		GlobalQPS:        10000,
		GlobalTPM:        1000000,
		GlobalTokenBurst: 1000,
		TenantTPM:        100,
		TenantTPMBurst:   10,
		TenantRPM:        60,
		TenantRPMBurst:   5,
		QueueSize:        100,
	}
	l := NewLimiter(cfg)
	l.SetRedis(rdb)
	defer l.Stop()

	if !l.CheckTenant("tenant-a", 5) {
		t.Error("Redis tenant should allow within burst")
	}
	for i := 0; i < 10; i++ {
		l.CheckTenant("tenant-a", 1)
	}
	if l.CheckTenant("tenant-a", 1) {
		t.Error("Redis tenant should reject after burst exhausted")
	}
	if !l.CheckTenant("tenant-b", 1) {
		t.Error("different tenant should not be affected")
	}
}

func TestLimiter_RedisProviderAllow(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	cfg := config.LimiterConfig{
		GlobalQPS:        10000,
		GlobalTPM:        1000000,
		GlobalTokenBurst: 1000,
		ProviderTPM:      100,
		ProviderTPMBurst: 10,
		ProviderRPM:      60,
		ProviderRPMBurst: 5,
		QueueSize:        100,
	}
	l := NewLimiter(cfg)
	l.SetRedis(rdb)
	defer l.Stop()

	if !l.CheckProvider("openai", 5) {
		t.Error("Redis provider should allow within burst")
	}
	for i := 0; i < 10; i++ {
		l.CheckProvider("openai", 1)
	}
	if l.CheckProvider("openai", 1) {
		t.Error("Redis provider should reject after burst exhausted")
	}
	if !l.CheckProvider("anthropic", 1) {
		t.Error("different provider should not be affected")
	}
}
