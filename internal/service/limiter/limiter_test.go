package limiter

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/config"
)

func TestTokenBucket_TryConsume(t *testing.T) {
	tb := NewTokenBucket(100, 10) // rate=100/s, burst=10

	// 初始 burst 为 10
	if !tb.TryConsume(1) {
		t.Error("should consume within burst")
	}

	// 消耗完 burst
	for i := 0; i < 9; i++ {
		tb.TryConsume(1)
	}

	// 现在 burst 耗尽，应该无法立即消费
	if tb.TryConsume(1) {
		t.Error("should not consume when burst exhausted")
	}
}

func TestTokenBucket_Refill(t *testing.T) {
	tb := NewTokenBucket(100, 5) // rate=100/s, burst=5

	// 消耗完 burst
	for i := 0; i < 5; i++ {
		tb.TryConsume(1)
	}

	// 等待补充 (rate=100/s, 100ms 约补充 10 个)
	time.Sleep(200 * time.Millisecond)

	// 再次尝试消费
	allowed := tb.TryConsume(1)
	t.Logf("refill test: allowed=%v", allowed)
	if !allowed {
		t.Error("should refill after waiting 200ms")
	}
}

func TestLimiter_GlobalQPS(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS: 100,
		GlobalTPM: 1000000,
		Burst:     50,
		QueueSize: 1000,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()

	// 验证可以消费
	if !l.Allow(ctx, "user1", 1) {
		t.Error("should allow within limit")
	}
}

func TestLimiter_PerUserQPS(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS: 10000, // 很高，测试 per-user
		GlobalTPM: 1000000,
		Burst:     1000,
		QueueSize: 1000,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()

	// 同一用户连续请求
	results := make(chan bool, 100)
	for i := 0; i < 50; i++ {
		go func() {
			results <- l.Allow(ctx, "user1", 1)
		}()
	}

	// 等待结果
	successCount := 0
	for i := 0; i < 50; i++ {
		if <-results {
			successCount++
		}
	}

	// 验证部分成功（在 burst 范围内）
	if successCount == 0 {
		t.Error("should have some successful requests")
	}
}

func TestLimiter_DifferentUsers(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS: 10000,
		GlobalTPM: 1000000,
		Burst:     1000,
		QueueSize: 1000,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()

	// 不同用户应该各自有独立的限流
	user1Allowed := l.Allow(ctx, "user1", 1)
	user2Allowed := l.Allow(ctx, "user2", 1)

	if !user1Allowed {
		t.Error("user1 should be allowed")
	}
	if !user2Allowed {
		t.Error("user2 should be allowed")
	}
}

func TestLimiter_QueueSize(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS: 1,    // 很低的全局 QPS
		GlobalTPM: 1000,
		Burst:     1,
		QueueSize: 5, // 小的队列
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()

	// 快速发送多个请求，测试队列
	for i := 0; i < 10; i++ {
		l.Allow(ctx, "user1", 1)
	}

	// 验证队列大小
	queueSize := l.QueueSize()
	if queueSize > 5 {
		t.Errorf("queue size should not exceed limit, got %d", queueSize)
	}
}

func TestLimiter_Concurrent(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS: 10000,
		GlobalTPM: 1000000,
		Burst:     1000,
		QueueSize: 1000,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()
	wg := sync.WaitGroup{}

	// 并发请求
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Allow(ctx, "user1", 1)
		}()
	}

	wg.Wait()

	// 不应该 panic
}

func TestLimiter_Cancel(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS: 10000,
		GlobalTPM: 1000000,
		Burst:     1000,
		QueueSize: 1000,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	// 已取消的 context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 应该返回 false
	if l.Allow(ctx, "user1", 1) {
		t.Error("should not allow when context is cancelled")
	}
}
