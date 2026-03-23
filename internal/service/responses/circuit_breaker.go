package responses

import (
	"fmt"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/config"
)

const (
	StateClosed    = "closed"
	StateOpen       = "open"
	StateHalfOpen   = "half-open"
)

type CircuitBreaker struct {
	mu        sync.RWMutex
	providers map[string]*ProviderState
	cfg       config.CircuitBreakerConfig
}

type ProviderState struct {
	failures          int
	lastFailure       time.Time
	state             string
	halfOpenRequests int // half-open 状态下的并发请求数
}

func NewCircuitBreaker(cfg config.CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		providers: make(map[string]*ProviderState),
		cfg:       cfg,
	}
}

func (cb *CircuitBreaker) key(tenantID, providerName string) string {
	return fmt.Sprintf("%s:%s", tenantID, providerName)
}

// TryAcquireHalfOpenRequest 尝试获取 half-open 探测请求的许可
// 返回 true 表示可以发起请求，false 表示被限制
func (cb *CircuitBreaker) TryAcquireHalfOpenRequest(tenantID, providerName string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	key := cb.key(tenantID, providerName)
	state, ok := cb.providers[key]
	if !ok {
		return true
	}

	if state.state != StateHalfOpen {
		return true
	}

	// 限制 half-open 状态下的并发请求数
	maxRequests := cb.cfg.HalfOpenMaxRequests
	if maxRequests <= 0 {
		maxRequests = 1
	}

	if state.halfOpenRequests >= maxRequests {
		return false
	}

	state.halfOpenRequests++
	return true
}

// ReleaseHalfOpenRequest 释放 half-open 探测请求
func (cb *CircuitBreaker) ReleaseHalfOpenRequest(tenantID, providerName string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	key := cb.key(tenantID, providerName)
	state, ok := cb.providers[key]
	if !ok {
		return
	}

	if state.halfOpenRequests > 0 {
		state.halfOpenRequests--
	}
}

func (cb *CircuitBreaker) IsAvailable(tenantID, providerName string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	key := cb.key(tenantID, providerName)
	state, ok := cb.providers[key]
	if !ok {
		return true
	}

	switch state.state {
	case StateClosed:
		return true
	case StateOpen:
		// 超过恢复超时，转为 half-open 状态
		if time.Since(state.lastFailure) > time.Duration(cb.cfg.RecoveryTimeout)*time.Second {
			state.state = StateHalfOpen
			return true
		}
		return false
	case StateHalfOpen:
		// half-open 状态下，限制并发请求数
		maxRequests := cb.cfg.HalfOpenMaxRequests
		if maxRequests <= 0 {
			maxRequests = 1
		}
		return state.halfOpenRequests < maxRequests
	default:
		return true
	}
}

func (cb *CircuitBreaker) RecordSuccess(tenantID, providerName string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	key := cb.key(tenantID, providerName)
	state, ok := cb.providers[key]
	if !ok {
		return
	}

	switch state.state {
	case StateHalfOpen:
		// half-open 状态下成功，恢复正常
		state.failures = 0
		state.state = StateClosed
	case StateClosed:
		// 正常状态下成功，重置失败计数
		state.failures = 0
	case StateOpen:
		// open 状态下成功，恢复正常
		state.failures = 0
		state.state = StateClosed
	}
}

func (cb *CircuitBreaker) RecordFailure(tenantID, providerName string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	key := cb.key(tenantID, providerName)
	state, ok := cb.providers[key]
	if !ok {
		state = &ProviderState{
			state: StateClosed,
		}
		cb.providers[key] = state
	}

	state.lastFailure = time.Now()

	switch state.state {
	case StateClosed:
		state.failures++
		if state.failures >= cb.cfg.FailureThreshold {
			state.state = StateOpen
		}
	case StateHalfOpen:
		// half-open 状态下失败，回到 open
		state.state = StateOpen
		state.failures = cb.cfg.FailureThreshold // 重置失败计数，下次从新开始
	case StateOpen:
		// 已经是 open，保持 open
	}
}

func (cb *CircuitBreaker) GetState(tenantID, providerName string) string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	state, ok := cb.providers[cb.key(tenantID, providerName)]
	if !ok {
		return StateClosed
	}
	return state.state
}
