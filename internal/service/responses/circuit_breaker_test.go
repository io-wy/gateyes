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
