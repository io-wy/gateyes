package budget

import (
	"context"
	"sync"
	"time"

	"gateyes/internal/dto"
)

type memoryBudgetEntry struct {
	value   int64
	resetAt time.Time
}

type MemoryBackend struct {
	mu     sync.Mutex
	values map[string]*memoryBudgetEntry
	now    func() time.Time
}

func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		values: make(map[string]*memoryBudgetEntry),
		now:    time.Now,
	}
}

func (s *MemoryBackend) Consume(_ context.Context, counters []dto.BudgetCounter) (dto.BudgetConsumeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.cleanup(now)

	maxRetry := time.Duration(0)
	failedKey := ""

	for _, counter := range counters {
		if counter.Key == "" || counter.Limit <= 0 || counter.Cost <= 0 {
			continue
		}
		entry := s.ensureEntry(counter.Key, normalizeTTL(counter.TTL), now)
		if entry.value+counter.Cost > counter.Limit {
			retry := entry.resetAt.Sub(now)
			if retry < 0 {
				retry = 0
			}
			if retry > maxRetry {
				maxRetry = retry
			}
			failedKey = counter.Key
		}
	}

	if failedKey != "" {
		return dto.BudgetConsumeResult{
			Allowed:    false,
			RetryAfter: maxRetry,
			FailedKey:  failedKey,
		}, nil
	}

	for _, counter := range counters {
		if counter.Key == "" || counter.Limit <= 0 || counter.Cost <= 0 {
			continue
		}
		entry := s.ensureEntry(counter.Key, normalizeTTL(counter.TTL), now)
		entry.value += counter.Cost
	}

	return dto.BudgetConsumeResult{Allowed: true}, nil
}

func (s *MemoryBackend) Adjust(_ context.Context, adjustments []dto.BudgetAdjustment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.cleanup(now)

	for _, adjustment := range adjustments {
		if adjustment.Key == "" || adjustment.Delta == 0 {
			continue
		}
		entry := s.ensureEntry(adjustment.Key, normalizeTTL(adjustment.TTL), now)
		entry.value += adjustment.Delta
		if entry.value < 0 {
			entry.value = 0
		}
	}

	return nil
}

func (s *MemoryBackend) ensureEntry(key string, ttl time.Duration, now time.Time) *memoryBudgetEntry {
	entry, ok := s.values[key]
	if !ok || now.After(entry.resetAt) {
		entry = &memoryBudgetEntry{value: 0, resetAt: now.Add(ttl)}
		s.values[key] = entry
	}
	return entry
}

func (s *MemoryBackend) cleanup(now time.Time) {
	for key, entry := range s.values {
		if now.After(entry.resetAt) {
			delete(s.values, key)
		}
	}
}
