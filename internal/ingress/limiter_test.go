package ingress

import (
	"testing"
	"time"

	"github.com/gateyes/gateway/internal/proxy"
)

func TestIngressLimiter_RPSBlocksAfterBurst(t *testing.T) {
	l := NewIngressLimiter()
	annot := &proxy.Annotations{
		RateLimitRPS: 1, // 1 request per second
	}

	// First request should be allowed.
	if !l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("first request should be allowed")
	}
	l.Release("route1", "10.0.0.1")

	// Second request immediately should also be allowed (burst = max(1*2, 10) = 10).
	if !l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("second request within burst should be allowed")
	}
	l.Release("route1", "10.0.0.1")

	// Exhaust burst.
	for i := 0; i < 8; i++ {
		if !l.Acquire("route1", "10.0.0.1", annot) {
			t.Fatalf("request %d within burst should be allowed", i+3)
		}
	}

	// Next request should be blocked (burst exhausted).
	if l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("request after burst should be blocked")
	}

	// Different client should still be allowed.
	if !l.Acquire("route1", "10.0.0.2", annot) {
		t.Fatal("different client should be allowed")
	}

	// Different route with same client should be allowed.
	if !l.Acquire("route2", "10.0.0.1", annot) {
		t.Fatal("different route should be allowed")
	}
}

func TestIngressLimiter_ConnectionLimitBlocks(t *testing.T) {
	l := NewIngressLimiter()
	annot := &proxy.Annotations{
		RateLimitConnections: 2, // max 2 concurrent connections
	}

	// First connection should be allowed.
	if !l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("first connection should be allowed")
	}

	// Second connection should be allowed.
	if !l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("second connection should be allowed")
	}

	// Third connection should be blocked.
	if l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("third connection should be blocked")
	}
}

func TestIngressLimiter_ReleaseAllowsNext(t *testing.T) {
	l := NewIngressLimiter()
	annot := &proxy.Annotations{
		RateLimitConnections: 1, // max 1 concurrent connection
	}

	// First acquire.
	if !l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("first acquire should be allowed")
	}

	// Second acquire should be blocked.
	if l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("second acquire should be blocked")
	}

	// Release first.
	l.Release("route1", "10.0.0.1")

	// Now second should be allowed.
	if !l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("acquire after release should be allowed")
	}
}

func TestIngressLimiter_NilAnnotations(t *testing.T) {
	l := NewIngressLimiter()

	// Should always allow with nil annotations.
	if !l.Acquire("route1", "10.0.0.1", nil) {
		t.Fatal("nil annotations should always allow")
	}
	// Release should not panic.
	l.Release("route1", "10.0.0.1")
}

func TestIngressLimiter_ZeroLimits(t *testing.T) {
	l := NewIngressLimiter()
	annot := &proxy.Annotations{
		RateLimitRPS:         0,
		RateLimitConnections: 0,
	}

	// Should always allow when limits are zero.
	for i := 0; i < 10; i++ {
		if !l.Acquire("route1", "10.0.0.1", annot) {
			t.Fatalf("request %d should be allowed with zero limits", i)
		}
	}
}

func TestIngressLimiter_RPSRefill(t *testing.T) {
	l := NewIngressLimiter()
	annot := &proxy.Annotations{
		RateLimitRPS: 1, // 1 per second
	}

	// Exhaust burst (burst = max(1*2, 10) = 10).
	allowed := 0
	for i := 0; i < 20; i++ {
		if l.Acquire("route1", "10.0.0.1", annot) {
			allowed++
		}
	}

	if allowed != 10 {
		t.Errorf("allowed = %d, want 10 (burst size)", allowed)
	}

	// Wait for refill.
	time.Sleep(1100 * time.Millisecond)

	// Should be able to acquire again.
	if !l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("request after refill should be allowed")
	}
}

func TestIngressLimiter_Isolation(t *testing.T) {
	l := NewIngressLimiter()
	annot := &proxy.Annotations{
		RateLimitConnections: 1,
	}

	// Client A acquires.
	if !l.Acquire("route1", "clientA", annot) {
		t.Fatal("clientA should acquire")
	}

	// Client B should also be able to acquire (different key).
	if !l.Acquire("route1", "clientB", annot) {
		t.Fatal("clientB should acquire")
	}

	// Client A second acquire should be blocked.
	if l.Acquire("route1", "clientA", annot) {
		t.Fatal("clientA second acquire should be blocked")
	}
}

func TestIngressLimiter_ReleaseUnderflow(t *testing.T) {
	l := NewIngressLimiter()
	annot := &proxy.Annotations{
		RateLimitConnections: 1,
	}

	// Release without acquire should not panic or go negative.
	l.Release("route1", "10.0.0.1")
	l.Release("route1", "10.0.0.1")

	// Should still be able to acquire.
	if !l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("acquire after extra releases should be allowed")
	}
}

func TestIngressLimiter_BothLimits(t *testing.T) {
	l := NewIngressLimiter()
	annot := &proxy.Annotations{
		RateLimitRPS:         100, // high RPS, shouldn't block
		RateLimitConnections: 1,   // connection limit is the bottleneck
	}

	// First should pass both.
	if !l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("first acquire should pass both limits")
	}

	// Second should be blocked by connection limit, not RPS.
	if l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("second acquire should be blocked by connection limit")
	}

	l.Release("route1", "10.0.0.1")

	// After release, should pass both again.
	if !l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("acquire after release should pass both limits")
	}
}

func TestIngressLimiter_ConcurrentAcquireRelease(t *testing.T) {
	l := NewIngressLimiter()
	annot := &proxy.Annotations{
		RateLimitConnections: 5,
	}

	// Spawn many goroutines trying to acquire (retry until success).
	acquired := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func() {
			for {
				ok := l.Acquire("route1", "10.0.0.1", annot)
				if ok {
					// Simulate some work then release.
					time.Sleep(10 * time.Millisecond)
					l.Release("route1", "10.0.0.1")
					acquired <- true
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}

	// Collect results.
	okCount := 0
	for i := 0; i < 100; i++ {
		if <-acquired {
			okCount++
		}
	}

	// All 100 should eventually succeed.
	if okCount != 100 {
		t.Errorf("acquired = %d, want 100", okCount)
	}
}

func TestIngressLimiter_AcquireWithoutConnectionLimit(t *testing.T) {
	l := NewIngressLimiter()
	annot := &proxy.Annotations{
		RateLimitRPS:         10,
		RateLimitConnections: 0,
	}

	// Acquire without connection limit should only check RPS.
	if !l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("first acquire should pass")
	}
	if !l.Acquire("route1", "10.0.0.1", annot) {
		t.Fatal("second acquire should pass")
	}
}

func TestIngressLimiter_HighRPSBurst(t *testing.T) {
	l := NewIngressLimiter()
	annot := &proxy.Annotations{
		RateLimitRPS: 50, // burst = max(50*2, 10) = 100
	}

	allowed := 0
	for i := 0; i < 120; i++ {
		if l.Acquire("route1", "10.0.0.1", annot) {
			allowed++
		}
	}

	if allowed != 100 {
		t.Errorf("allowed = %d, want 100 (burst size for RPS=50)", allowed)
	}
}

// BenchmarkIngressLimiter_Acquire measures limiter performance.
func BenchmarkIngressLimiter_Acquire(b *testing.B) {
	l := NewIngressLimiter()
	annot := &proxy.Annotations{
		RateLimitRPS:         1000,
		RateLimitConnections: 100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Acquire("route1", "10.0.0.1", annot)
	}
}
