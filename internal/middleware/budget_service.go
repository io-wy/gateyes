package middleware

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type budgetCounter struct {
	Key   string
	Limit int64
	Cost  int64
	TTL   time.Duration
}

type budgetAdjustment struct {
	Key   string
	Delta int64
	TTL   time.Duration
}

type budgetConsumeResult struct {
	Allowed    bool
	RetryAfter time.Duration
	FailedKey  string
}

type BudgetService interface {
	Consume(ctx context.Context, counters []budgetCounter) (budgetConsumeResult, error)
	Adjust(ctx context.Context, adjustments []budgetAdjustment) error
}

type memoryBudgetEntry struct {
	value   int64
	resetAt time.Time
}

type MemoryBudgetService struct {
	mu     sync.Mutex
	values map[string]*memoryBudgetEntry
	now    func() time.Time
}

func NewMemoryBudgetService() *MemoryBudgetService {
	return &MemoryBudgetService{
		values: make(map[string]*memoryBudgetEntry),
		now:    time.Now,
	}
}

func (s *MemoryBudgetService) Consume(_ context.Context, counters []budgetCounter) (budgetConsumeResult, error) {
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
		ttl := normalizeTTL(counter.TTL)
		entry := s.ensureEntry(counter.Key, ttl, now)
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
		return budgetConsumeResult{
			Allowed:    false,
			RetryAfter: maxRetry,
			FailedKey:  failedKey,
		}, nil
	}

	for _, counter := range counters {
		if counter.Key == "" || counter.Limit <= 0 || counter.Cost <= 0 {
			continue
		}
		ttl := normalizeTTL(counter.TTL)
		entry := s.ensureEntry(counter.Key, ttl, now)
		entry.value += counter.Cost
	}

	return budgetConsumeResult{Allowed: true}, nil
}

func (s *MemoryBudgetService) Adjust(_ context.Context, adjustments []budgetAdjustment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.cleanup(now)

	for _, adjustment := range adjustments {
		if adjustment.Key == "" || adjustment.Delta == 0 {
			continue
		}
		ttl := normalizeTTL(adjustment.TTL)
		entry := s.ensureEntry(adjustment.Key, ttl, now)
		entry.value += adjustment.Delta
		if entry.value < 0 {
			entry.value = 0
		}
	}

	return nil
}

func (s *MemoryBudgetService) ensureEntry(key string, ttl time.Duration, now time.Time) *memoryBudgetEntry {
	entry, ok := s.values[key]
	if !ok || now.After(entry.resetAt) {
		entry = &memoryBudgetEntry{value: 0, resetAt: now.Add(ttl)}
		s.values[key] = entry
	}
	return entry
}

func (s *MemoryBudgetService) cleanup(now time.Time) {
	for key, entry := range s.values {
		if now.After(entry.resetAt) {
			delete(s.values, key)
		}
	}
}

type RedisBudgetService struct {
	client *redis.Client
	prefix string
}

var sharedRedisClients sync.Map

func NewRedisBudgetService(addr, password string, db int, prefix string) (*RedisBudgetService, error) {
	if strings.TrimSpace(addr) == "" {
		return nil, errors.New("redis address is required")
	}

	client, err := getSharedRedisClient(addr, password, db)
	if err != nil {
		return nil, err
	}

	return &RedisBudgetService{
		client: client,
		prefix: strings.TrimSpace(prefix),
	}, nil
}

func getSharedRedisClient(addr, password string, db int) (*redis.Client, error) {
	cacheKey := fmt.Sprintf("%s|%d|%s", strings.TrimSpace(addr), db, password)
	if existing, ok := sharedRedisClients.Load(cacheKey); ok {
		if client, ok := existing.(*redis.Client); ok {
			return client, nil
		}
	}

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     128,
		MinIdleConns: 16,
		PoolTimeout:  2 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	actual, loaded := sharedRedisClients.LoadOrStore(cacheKey, client)
	if loaded {
		_ = client.Close()
		if shared, ok := actual.(*redis.Client); ok {
			return shared, nil
		}
	}
	return client, nil
}

func (s *RedisBudgetService) Consume(ctx context.Context, counters []budgetCounter) (budgetConsumeResult, error) {
	keys := make([]string, 0, len(counters))
	args := make([]interface{}, 0, len(counters)*3)

	for _, counter := range counters {
		if counter.Key == "" || counter.Limit <= 0 || counter.Cost <= 0 {
			continue
		}

		keys = append(keys, s.fullKey(counter.Key))
		args = append(args,
			counter.Cost,
			counter.Limit,
			int64(normalizeTTL(counter.TTL).Seconds()),
		)
	}

	if len(keys) == 0 {
		return budgetConsumeResult{Allowed: true}, nil
	}

	result, err := redisConsumeScript.Run(ctx, s.client, keys, args...).Result()
	if err != nil {
		return budgetConsumeResult{}, err
	}

	raw, ok := result.([]interface{})
	if !ok || len(raw) < 2 {
		return budgetConsumeResult{}, errors.New("unexpected redis consume response")
	}

	allowed, err := toInt64(raw[0])
	if err != nil {
		return budgetConsumeResult{}, err
	}
	if allowed == 1 {
		return budgetConsumeResult{Allowed: true}, nil
	}

	retrySeconds, err := toInt64(raw[1])
	if err != nil {
		return budgetConsumeResult{}, err
	}

	failedKey := ""
	if len(raw) >= 3 {
		if keyStr, ok := raw[2].(string); ok {
			failedKey = strings.TrimPrefix(keyStr, s.prefix+":")
		}
	}

	return budgetConsumeResult{
		Allowed:    false,
		RetryAfter: time.Duration(retrySeconds) * time.Second,
		FailedKey:  failedKey,
	}, nil
}

func (s *RedisBudgetService) Adjust(ctx context.Context, adjustments []budgetAdjustment) error {
	keys := make([]string, 0, len(adjustments))
	args := make([]interface{}, 0, len(adjustments)*2)

	for _, adjustment := range adjustments {
		if adjustment.Key == "" || adjustment.Delta == 0 {
			continue
		}
		keys = append(keys, s.fullKey(adjustment.Key))
		args = append(args,
			adjustment.Delta,
			int64(normalizeTTL(adjustment.TTL).Seconds()),
		)
	}

	if len(keys) == 0 {
		return nil
	}

	return redisAdjustScript.Run(ctx, s.client, keys, args...).Err()
}

func (s *RedisBudgetService) fullKey(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + ":" + key
}

func normalizeTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return time.Minute
	}
	return ttl
}

func toInt64(v interface{}) (int64, error) {
	switch value := v.(type) {
	case int64:
		return value, nil
	case int:
		return int64(value), nil
	case float64:
		return int64(value), nil
	case string:
		return strconv.ParseInt(value, 10, 64)
	default:
		return 0, fmt.Errorf("unsupported integer type %T", v)
	}
}

var redisConsumeScript = redis.NewScript(`
local n = #KEYS
for i = 1, n do
  local base = (i - 1) * 3
  local cost = tonumber(ARGV[base + 1]) or 0
  local limit = tonumber(ARGV[base + 2]) or 0
  local ttl = tonumber(ARGV[base + 3]) or 1
  local current = tonumber(redis.call("GET", KEYS[i]) or "0")
  if current + cost > limit then
    local retry = redis.call("TTL", KEYS[i])
    if retry < 0 then retry = ttl end
    return {0, retry, KEYS[i]}
  end
end

for i = 1, n do
  local base = (i - 1) * 3
  local cost = tonumber(ARGV[base + 1]) or 0
  local ttl = tonumber(ARGV[base + 3]) or 1
  redis.call("INCRBY", KEYS[i], cost)
  if redis.call("TTL", KEYS[i]) < 0 then
    redis.call("EXPIRE", KEYS[i], ttl)
  end
end

return {1, 0, ""}
`)

var redisAdjustScript = redis.NewScript(`
local n = #KEYS
for i = 1, n do
  local base = (i - 1) * 2
  local delta = tonumber(ARGV[base + 1]) or 0
  local ttl = tonumber(ARGV[base + 2]) or 1
  if delta ~= 0 then
    local next = redis.call("INCRBY", KEYS[i], delta)
    if next < 0 then
      redis.call("SET", KEYS[i], 0)
    end
    if redis.call("TTL", KEYS[i]) < 0 then
      redis.call("EXPIRE", KEYS[i], ttl)
    end
  end
end
return 1
`)
