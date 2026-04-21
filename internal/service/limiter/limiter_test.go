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
		GlobalQPS:           100,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    50,
		PerUserRequestBurst: 50,
		QueueSize:           1000,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()

	// 验证可以消费: userQPS=0 使用全局默认，admissionTokens=1
	if !l.Allow(ctx, "user1", 0, 1) {
		t.Error("should allow within limit")
	}
}

func TestLimiter_PerUserQPS(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:           10000, // 很高，测试 per-user
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000,
		PerUserRequestBurst: 1000,
		QueueSize:           1000,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()

	// 同一用户连续请求
	results := make(chan bool, 50)
	for i := 0; i < 50; i++ {
		go func() {
			// userQPS=10 表示限制该用户每秒 10 请求
			results <- l.Allow(ctx, "user1", 10, 1)
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
	t.Logf("per-user QPS test: success=%d, total=50", successCount)
}

func TestLimiter_DifferentUsers(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:           10000,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000,
		PerUserRequestBurst: 1000,
		QueueSize:           1000,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()

	// 不同用户应该各自有独立的限流
	user1Allowed := l.Allow(ctx, "user1", 0, 1)
	user2Allowed := l.Allow(ctx, "user2", 0, 1)

	if !user1Allowed {
		t.Error("user1 should be allowed")
	}
	if !user2Allowed {
		t.Error("user2 should be allowed")
	}
}

func TestLimiter_QueueSize(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:           1, // 很低的全局 QPS
		GlobalTPM:           1000,
		GlobalTokenBurst:    1,
		PerUserRequestBurst: 1,
		QueueSize:           5, // 小的队列
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()

	// 快速发送多个请求，测试队列
	for i := 0; i < 10; i++ {
		l.Allow(ctx, "user1", 0, 1)
	}

	// 验证队列大小
	queueSize := l.QueueSize()
	if queueSize > 5 {
		t.Errorf("queue size should not exceed limit, got %d", queueSize)
	}
}

func TestLimiter_Concurrent(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:           10000,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000,
		PerUserRequestBurst: 1000,
		QueueSize:           1000,
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
			l.Allow(ctx, "user1", 0, 1)
		}()
	}

	wg.Wait()

	// 不应该 panic
}

func TestLimiter_Cancel(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:           10000,
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000,
		PerUserRequestBurst: 1000,
		QueueSize:           1000,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	// 已取消的 context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 应该返回 false
	if l.Allow(ctx, "user1", 0, 1) {
		t.Error("should not allow when context is cancelled")
	}
}

func TestLimiter_UserQPSConfig(t *testing.T) {
	cfg := config.LimiterConfig{
		GlobalQPS:           5, // 全局默认只有 5 QPS
		GlobalTPM:           1000000,
		GlobalTokenBurst:    1000,
		PerUserRequestBurst: 10,
		QueueSize:           1000,
	}
	l := NewLimiter(cfg)
	defer l.Stop()

	ctx := context.Background()

	// 用户配置了 100 QPS，应该使用用户配置而非全局默认
	// 快速发送 20 个请求，用户 QPS=100 应该有更多通过
	successCount := 0
	for i := 0; i < 20; i++ {
		if l.Allow(ctx, "user1", 100, 1) { // userQPS=100
			successCount++
		}
	}

	// 使用全局默认 5 QPS 的话，20 个请求只能通过约 5-10 个（受 burst 影响）
	// 使用用户配置 100 QPS 的话，应该能通过更多
	t.Logf("userQPS config test: userQPS=100, success=%d/20", successCount)
	if successCount < 10 {
		t.Error("user configured QPS should allow more requests than global default")
	}
}
