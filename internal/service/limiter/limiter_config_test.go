package limiter

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestLimiter_Allow(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:           100,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    50,
		PerUserRequestBurst: 50,
		QueueSize:           100,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()
	if !l.Allow(ctx, "user1", 0, 1) {
		t.Error("first request should be allowed")
	}
	if !l.Allow(ctx, "user2", 0, 1) {
		t.Error("second request should be allowed")
	}
}

func TestLimiter_CheckModel(t *testing.T) {
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
	defer l.Stop()

	if !l.CheckModel("gpt-4", 5) {
		t.Error("model should allow within burst")
	}
	for i := 0; i < 10; i++ {
		l.CheckModel("gpt-4", 1)
	}
	if l.CheckModel("gpt-4", 1) {
		t.Error("model should reject after burst exhausted")
	}
	if !l.CheckModel("claude", 1) {
		t.Error("different model should not be affected")
	}
}

func TestLimiter_CheckProvider(t *testing.T) {
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
	defer l.Stop()

	if !l.CheckProvider("openai", 5) {
		t.Error("provider should allow within burst")
	}
	for i := 0; i < 10; i++ {
		l.CheckProvider("openai", 1)
	}
	if l.CheckProvider("openai", 1) {
		t.Error("provider should reject after burst exhausted")
	}
	if !l.CheckProvider("anthropic", 1) {
		t.Error("different provider should not be affected")
	}
}

func TestLimiter_CheckTenant(t *testing.T) {
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
	defer l.Stop()

	if !l.CheckTenant("tenant-a", 5) {
		t.Error("tenant should allow within burst")
	}
	for i := 0; i < 10; i++ {
		l.CheckTenant("tenant-a", 1)
	}
	if l.CheckTenant("tenant-a", 1) {
		t.Error("tenant should reject after burst exhausted")
	}
	if !l.CheckTenant("tenant-b", 1) {
		t.Error("different tenant should not be affected")
	}
}

func TestLimiter_Stop(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:        100,
		GlobalTPM:        1000000,
		GlobalTokenBurst: 50,
		QueueSize:        100,
	}
	l := NewLimiter(cfg)
	l.Stop()

	// Verify goroutines exited by checking queue is empty and no panic on second Stop
	// Second Stop panics because stopCh is already closed; we test first Stop succeeds
	if l.QueueSize() != 0 {
		t.Error("queue should be empty after Stop")
	}
}

func TestLimiter_Reload(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:        100,
		GlobalTPM:        1000000,
		GlobalTokenBurst: 50,
		QueueSize:        100,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	newCfg := &config.Config{
		Limiter: config.LimiterConfig{
			GlobalQPS:        200,
			GlobalTPM:        2000000,
			GlobalTokenBurst: 100,
			QueueSize:        200,
		},
	}
	if err := l.Reload(newCfg); err != nil {
		t.Fatalf("Reload error: %v", err)
	}
	if l.cfg.GlobalQPS != 200 {
		t.Errorf("GlobalQPS = %d, want 200", l.cfg.GlobalQPS)
	}
}

func TestLimiter_QueueSizeReflectsPending(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:           1,
		GlobalTPM:           1000,
		GlobalTokenBurst:    1,
		PerUserRequestBurst: 1,
		QueueSize:           3,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		l.Allow(ctx, "user", 0, 1)
	}
	if l.QueueSize() > 3 {
		t.Errorf("queue size %d exceeds limit 3", l.QueueSize())
	}
}

func TestLimiter_RedisFailureFallback(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:        10000,
		GlobalTPM:        1000000,
		GlobalTokenBurst: 1000,
		TenantTPM:        100,
		TenantTPMBurst:   10,
		QueueSize:        100,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	// No Redis set; local bucket should continue serving
	if !l.CheckTenant("tenant-fallback", 1) {
		t.Error("local bucket should serve when Redis is unavailable")
	}
}

func TestLimiter_SetRedisDynamicSwap(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:        100,
		GlobalTPM:        1000000,
		GlobalTokenBurst: 100,
		QueueSize:        100,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	if l.rdb != nil {
		t.Error("rdb should be nil initially")
	}
	l.SetRedis(nil)
	if l.rdb != nil {
		t.Error("SetRedis(nil) should set rdb to nil")
	}
}
