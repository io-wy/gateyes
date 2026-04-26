package alert

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/gateyes/gateway/internal/config"
)

func TestAlertAggregator_RedisDedup(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	agg := NewAlertAggregator(5 * time.Second)
	agg.SetRedis(rdb)

	if !agg.ShouldSend("event:provider:openai") {
		t.Error("first send should be allowed")
	}
	if agg.ShouldSend("event:provider:openai") {
		t.Error("duplicate within window should be blocked")
	}
	if !agg.ShouldSend("event:provider:anthropic") {
		t.Error("different key should be allowed")
	}

	mr.FastForward(6 * time.Second)
	if !agg.ShouldSend("event:provider:openai") {
		t.Error("after window expires, send should be allowed again")
	}
}

func TestAlertAggregator_RedisFallbackToLocal(t *testing.T) {
	agg := NewAlertAggregator(5 * time.Minute)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:0", MaxRetries: 0, DialTimeout: 100 * time.Millisecond})
	defer rdb.Close()
	agg.SetRedis(rdb)

	if !agg.ShouldSend("key1") {
		t.Error("should fall back to local on Redis error")
	}
	if agg.ShouldSend("key1") {
		t.Error("local fallback should still dedup")
	}
}

func TestAlertAggregator_CleanupNoopWithRedis(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	agg := NewAlertAggregator(50 * time.Millisecond)
	agg.SetRedis(rdb)
	agg.ShouldSend("key1")
	agg.Cleanup()

	agg.mu.RLock()
	len := len(agg.states)
	agg.mu.RUnlock()
	if len != 0 {
		t.Error("Cleanup should be no-op with Redis, states should stay empty")
	}
}

func TestAlertService_SetRedis(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	svc := NewAlertService(config.AlertConfig{Enabled: true}, nil)
	svc.SetRedis(rdb)

	if svc.aggregator.rdb == nil {
		t.Error("SetRedis should propagate to aggregator")
	}
}
