package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gateyes/gateway/internal/app/config"
	"github.com/redis/go-redis/v9"
)

const (
	StateClosed   = "closed"
	StateOpen     = "open"
	StateHalfOpen = "half-open"
)

type CircuitBreaker struct {
	mu        sync.RWMutex
	providers map[string]*ProviderState
	cfg       config.CircuitBreakerConfig
	rdb       *redis.Client
}

type ProviderState struct {
	failures         int
	lastFailure      time.Time
	state            string
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

// GetAllStates returns all provider states for metrics collection
// Returns a map with key "tenantID:providerName" -> state string
func (cb *CircuitBreaker) GetAllStates() map[string]int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	result := make(map[string]int)
	for key, state := range cb.providers {
		// Convert state string to numeric value for Gauge
		var stateValue int
		switch state.state {
		case StateClosed:
			stateValue = 0
		case StateOpen:
			stateValue = 1
		case StateHalfOpen:
			stateValue = 2
		default:
			stateValue = 0
		}
		result[key] = stateValue
	}
	return result
}

// SetRedis enables state persistence across restarts.
func (cb *CircuitBreaker) SetRedis(rdb *redis.Client) {
	cb.rdb = rdb
}

// PersistState serializes current states to Redis with a 5-minute TTL.
func (cb *CircuitBreaker) PersistState(ctx context.Context) {
	if cb.rdb == nil {
		return
	}
	cb.mu.RLock()
	states := make(map[string]int, len(cb.providers))
	for k, s := range cb.providers {
		var v int
		switch s.state {
		case StateClosed:
			v = 0
		case StateOpen:
			v = 1
		case StateHalfOpen:
			v = 2
		}
		states[k] = v
	}
	cb.mu.RUnlock()

	data, _ := json.Marshal(states)
	cb.rdb.Set(ctx, "circuit_breaker:states", data, 5*time.Minute)
}

// RestoreState loads states from Redis after restart.
func (cb *CircuitBreaker) RestoreState(ctx context.Context) {
	if cb.rdb == nil {
		return
	}
	data, err := cb.rdb.Get(ctx, "circuit_breaker:states").Result()
	if err != nil {
		return
	}
	var states map[string]int
	if err := json.Unmarshal([]byte(data), &states); err != nil {
		return
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()
	for key, v := range states {
		var stateStr string
		switch v {
		case 1:
			stateStr = StateOpen
		case 2:
			stateStr = StateHalfOpen
		default:
			stateStr = StateClosed
		}
		if _, ok := cb.providers[key]; !ok {
			cb.providers[key] = &ProviderState{state: stateStr}
		} else {
			cb.providers[key].state = stateStr
		}
		// Mark open states with a recent failure time so they don't immediately flip to half-open.
		if stateStr == StateOpen {
			cb.providers[key].lastFailure = time.Now()
		}
	}
}
