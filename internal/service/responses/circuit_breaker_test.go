package responses

import (
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestCircuitBreaker_IsAvailable(t *testing.T) {
	cfg := config.CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  1,
	}
	cb := NewCircuitBreaker(cfg)

	tenantID := "tenant-1"
	providerName := "provider-1"

	// 初始状态应该可用
	if !cb.IsAvailable(tenantID, providerName) {
		t.Error("expected available initially")
	}

	// 失败未达到阈值，应该仍然可用
	cb.RecordFailure(tenantID, providerName)
	cb.RecordFailure(tenantID, providerName)
	if !cb.IsAvailable(tenantID, providerName) {
		t.Error("expected available after 2 failures (below threshold)")
	}

	// 达到阈值，熔断器应该打开
	cb.RecordFailure(tenantID, providerName)
	if cb.IsAvailable(tenantID, providerName) {
		t.Error("expected unavailable after 3 failures (reached threshold)")
	}

	// 成功应该重置状态
	cb.RecordSuccess(tenantID, providerName)
	if !cb.IsAvailable(tenantID, providerName) {
		t.Error("expected available after success")
	}

	// 验证状态为 closed
	if cb.GetState(tenantID, providerName) != StateClosed {
		t.Errorf("expected state closed, got %s", cb.GetState(tenantID, providerName))
	}
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	cfg := config.CircuitBreakerConfig{
		FailureThreshold: 2,
		RecoveryTimeout:  1,
	}
	cb := NewCircuitBreaker(cfg)

	tenantID := "tenant-1"
	providerName := "provider-1"

	// closed -> open
	cb.RecordFailure(tenantID, providerName)
	cb.RecordFailure(tenantID, providerName)
	if cb.GetState(tenantID, providerName) != StateOpen {
		t.Errorf("expected state open, got %s", cb.GetState(tenantID, providerName))
	}

	// open -> half-open (after recovery timeout)
	// 注意: 这里不测试实际的时间等待
}

func TestCircuitBreaker_KeyFormat(t *testing.T) {
	cfg := config.CircuitBreakerConfig{}
	cb := NewCircuitBreaker(cfg)

	key := cb.key("tenant-a", "provider-b")
	if key != "tenant-a:provider-b" {
		t.Errorf("expected key 'tenant-a:provider-b', got '%s'", key)
	}
}

func TestCircuitBreaker_DifferentProviders(t *testing.T) {
	cfg := config.CircuitBreakerConfig{
		FailureThreshold: 2,
	}
	cb := NewCircuitBreaker(cfg)

	tenantID := "tenant-1"

	// provider-1 熔断
	cb.RecordFailure(tenantID, "provider-1")
	cb.RecordFailure(tenantID, "provider-1")

	// provider-2 不应该受影响
	if !cb.IsAvailable(tenantID, "provider-2") {
		t.Error("expected provider-2 to be available")
	}
}

func TestTryAcquireHalfOpenRequestAllowsWhenNotHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(config.CircuitBreakerConfig{})
	if !cb.TryAcquireHalfOpenRequest("t1", "p1") {
		t.Fatal("expected true when state not half-open")
	}
}

func TestTryAcquireHalfOpenRequestLimitsConcurrency(t *testing.T) {
	cb := NewCircuitBreaker(config.CircuitBreakerConfig{FailureThreshold: 1, RecoveryTimeout: 1, HalfOpenMaxRequests: 2})
	cb.RecordFailure("t1", "p1")
	// Simulate half-open by manipulating state directly through IsAvailable timing
	// Instead, use RecordFailure + direct state access via IsAvailable transition
	// Force half-open by checking availability after timeout would have passed
	// Since we can't wait, we test the limit by creating a state manually
	cb.mu.Lock()
	cb.providers["t1:p1"].state = StateHalfOpen
	cb.providers["t1:p1"].halfOpenRequests = 2
	cb.mu.Unlock()

	if cb.TryAcquireHalfOpenRequest("t1", "p1") {
		t.Fatal("expected false when half-open requests at limit")
	}
}

func TestReleaseHalfOpenRequestDecrementsCount(t *testing.T) {
	cb := NewCircuitBreaker(config.CircuitBreakerConfig{HalfOpenMaxRequests: 2})
	cb.mu.Lock()
	cb.providers["t1:p1"] = &ProviderState{state: StateHalfOpen, halfOpenRequests: 2}
	cb.mu.Unlock()

	cb.ReleaseHalfOpenRequest("t1", "p1")
	cb.mu.Lock()
	got := cb.providers["t1:p1"].halfOpenRequests
	cb.mu.Unlock()
	if got != 1 {
		t.Fatalf("halfOpenRequests = %d, want 1", got)
	}
}

func TestReleaseHalfOpenRequestNoOpForMissing(t *testing.T) {
	cb := NewCircuitBreaker(config.CircuitBreakerConfig{})
	cb.ReleaseHalfOpenRequest("t1", "p1") // should not panic
}

func TestGetAllStatesReturnsNumericValues(t *testing.T) {
	cb := NewCircuitBreaker(config.CircuitBreakerConfig{FailureThreshold: 1})
	cb.RecordFailure("t1", "p1")
	states := cb.GetAllStates()
	if len(states) != 1 {
		t.Fatalf("len(states) = %d, want 1", len(states))
	}
	if states["t1:p1"] != 1 {
		t.Fatalf("state value = %d, want 1 (open)", states["t1:p1"])
	}
}

func TestGetAllStatesEmptyWhenNoProviders(t *testing.T) {
	cb := NewCircuitBreaker(config.CircuitBreakerConfig{})
	states := cb.GetAllStates()
	if len(states) != 0 {
		t.Fatalf("len(states) = %d, want 0", len(states))
	}
}
