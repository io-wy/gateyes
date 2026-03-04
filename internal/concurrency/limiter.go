package concurrency

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeChannel Scope = "channel"
	ScopeToken   Scope = "token"
)

type AcquireKeys struct {
	ChannelID string
	TokenID   string
}

type ReleaseFunc func()

type Manager interface {
	Acquire(ctx context.Context, keys AcquireKeys) (ReleaseFunc, error)
}

type Limits struct {
	Global     int
	PerChannel int
	PerToken   int
}

var ErrLimitExceeded = errors.New("concurrency limit exceeded")

type LimitError struct {
	Scope Scope
	Key   string
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("%s: scope=%s key=%s", ErrLimitExceeded.Error(), e.Scope, e.Key)
}

func (e *LimitError) Unwrap() error {
	return ErrLimitExceeded
}

type MemoryManager struct {
	global *semaphore

	perChannel int
	perToken   int

	mu             sync.Mutex
	channelBuckets map[string]*semaphore
	tokenBuckets   map[string]*semaphore
}

func NewMemoryManager(limits Limits) *MemoryManager {
	return &MemoryManager{
		global:         newSemaphore(limits.Global),
		perChannel:     limits.PerChannel,
		perToken:       limits.PerToken,
		channelBuckets: make(map[string]*semaphore),
		tokenBuckets:   make(map[string]*semaphore),
	}
}

func (m *MemoryManager) Acquire(_ context.Context, keys AcquireKeys) (ReleaseFunc, error) {
	acquired := make([]*semaphore, 0, 3)

	if !m.global.TryAcquire() {
		return nil, &LimitError{Scope: ScopeGlobal, Key: "global"}
	}
	acquired = append(acquired, m.global)

	if keys.ChannelID != "" {
		channelSem := m.channelSemaphore(keys.ChannelID)
		if !channelSem.TryAcquire() {
			releaseAll(acquired)
			return nil, &LimitError{Scope: ScopeChannel, Key: keys.ChannelID}
		}
		acquired = append(acquired, channelSem)
	}

	if keys.TokenID != "" {
		tokenSem := m.tokenSemaphore(keys.TokenID)
		if !tokenSem.TryAcquire() {
			releaseAll(acquired)
			return nil, &LimitError{Scope: ScopeToken, Key: keys.TokenID}
		}
		acquired = append(acquired, tokenSem)
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			releaseAll(acquired)
		})
	}, nil
}

func (m *MemoryManager) channelSemaphore(channelID string) *semaphore {
	m.mu.Lock()
	defer m.mu.Unlock()

	sem, ok := m.channelBuckets[channelID]
	if ok {
		return sem
	}
	sem = newSemaphore(m.perChannel)
	m.channelBuckets[channelID] = sem
	return sem
}

func (m *MemoryManager) tokenSemaphore(tokenID string) *semaphore {
	m.mu.Lock()
	defer m.mu.Unlock()

	sem, ok := m.tokenBuckets[tokenID]
	if ok {
		return sem
	}
	sem = newSemaphore(m.perToken)
	m.tokenBuckets[tokenID] = sem
	return sem
}

func releaseAll(list []*semaphore) {
	for i := len(list) - 1; i >= 0; i-- {
		list[i].Release()
	}
}

type semaphore struct {
	ch chan struct{}
}

func newSemaphore(limit int) *semaphore {
	if limit <= 0 {
		// TODO(io): support true "unlimited" with metrics visibility.
		limit = 1
	}
	return &semaphore{ch: make(chan struct{}, limit)}
}

func (s *semaphore) TryAcquire() bool {
	select {
	case s.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *semaphore) Release() {
	select {
	case <-s.ch:
	default:
	}
}
