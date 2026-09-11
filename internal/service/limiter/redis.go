package limiter

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// tokenBucketLua implements an atomic token bucket in Redis.
// KEYS[1] = bucket key
// ARGV[1] = rate (tokens/second), ARGV[2] = burst, ARGV[3] = consume, ARGV[4] = now_ms
// Returns 1 if allowed, 0 if denied.
var tokenBucketLua = redis.NewScript(`
local key = KEYS[1]
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local consume = tonumber(ARGV[3])
local now = tonumber(ARGV[4])

local tokens = tonumber(redis.call('HGET', key, 't'))
local last_fill = tonumber(redis.call('HGET', key, 'l'))

if tokens == nil then tokens = burst end
if last_fill == nil then last_fill = now end

local elapsed = math.max(0, now - last_fill)
tokens = math.min(burst, tokens + (rate * elapsed) / 1000)

local ok = 0
if tokens >= consume then
    tokens = tokens - consume
    ok = 1
end

redis.call('HSET', key, 't', tokens, 'l', now)
redis.call('EXPIRE', key, 120)

return ok
`)

// multiBucketLua atomically evaluates all buckets for one request.
// KEYS[i] = bucket key.
// ARGV[1] = now_ms, ARGV[2] = bucket_count.
// Then each bucket contributes: rate, burst, consume, reason, scope.
// Returns {1, "", ""} when allowed, otherwise {0, reason, scope}.
var multiBucketLua = redis.NewScript(`
local now = tonumber(ARGV[1])
local count = tonumber(ARGV[2])
local tokens = {}
local last_fills = {}

for i = 1, count do
    local offset = 2 + ((i - 1) * 5)
    local rate = tonumber(ARGV[offset + 1])
    local burst = tonumber(ARGV[offset + 2])
    local consume = tonumber(ARGV[offset + 3])
    local reason = ARGV[offset + 4]
    local scope = ARGV[offset + 5]
    local key = KEYS[i]

    local current = tonumber(redis.call('HGET', key, 't'))
    local last_fill = tonumber(redis.call('HGET', key, 'l'))
    if current == nil then current = burst end
    if last_fill == nil then last_fill = now end

    local elapsed = math.max(0, now - last_fill)
    current = math.min(burst, current + (rate * elapsed) / 1000)

    tokens[i] = current
    last_fills[i] = last_fill

    if current < consume then
        return {0, reason, scope}
    end
end

for i = 1, count do
    local offset = 2 + ((i - 1) * 5)
    local consume = tonumber(ARGV[offset + 3])
    redis.call('HSET', KEYS[i], 't', tokens[i] - consume, 'l', now)
    redis.call('EXPIRE', KEYS[i], 120)
end

return {1, "", ""}
`)

func redisTryConsume(rdb *redis.Client, redisKey string, n int, rate float64, burst int) bool {
	if rate <= 0 || burst <= 0 {
		return true
	}
	now := time.Now().UnixMilli()
	result, err := tokenBucketLua.Run(context.Background(), rdb, []string{redisKey}, rate, burst, n, now).Int()
	if err != nil {
		return true
	}
	return result == 1
}

func redisCheck(ctx context.Context, rdb *redis.Client, specs []limitSpec) Decision {
	keys := make([]string, 0, len(specs))
	args := make([]any, 0, 2+len(specs)*5)
	args = append(args, time.Now().UnixMilli(), len(specs))
	for _, spec := range specs {
		keys = append(keys, spec.key)
		args = append(args, spec.rate, spec.burst, spec.consume, spec.reason, spec.scope)
	}

	result, err := multiBucketLua.Run(ctx, rdb, keys, args...).Slice()
	if err != nil {
		return deniedFromErr(err)
	}
	if len(result) == 0 {
		return Decision{Allowed: false, Reason: "redis_error"}
	}
	allowed, _ := result[0].(int64)
	if allowed == 1 {
		return Decision{Allowed: true}
	}
	decision := Decision{Allowed: false}
	if len(result) > 1 {
		decision.Reason, _ = result[1].(string)
	}
	if len(result) > 2 {
		decision.Scope, _ = result[2].(string)
	}
	if decision.Reason == "" {
		decision.Reason = "rate_limited"
	}
	return decision
}

func limiterKey(parts ...string) string {
	// Use the most specific identifier as the Redis Cluster hash tag
	// so that related keys (e.g. tenant TPM and RPM) land on the same slot.
	tag := parts[0]
	if len(parts) >= 2 && parts[1] != "" {
		tag = parts[1]
	}
	return fmt.Sprintf("gateyes:rl:{%s}:%s", tag, joinKey(parts))
}

func atomicLimiterKey(scope, id, metric string) string {
	if id == "" {
		id = scope
	}
	// TODO(rate-limit-hotspot): all keys in one Lua call need the same Redis
	// Cluster hash slot. This fixed tag is correct for HPA semantics but can
	// hotspot under very high load; shard/partition this after correctness.
	return fmt.Sprintf("gateyes:rl:{all}:%s:%s:%s", scope, id, metric)
}

func joinKey(parts []string) string {
	s := ""
	for _, p := range parts {
		if s != "" {
			s += ":"
		}
		s += p
	}
	return s
}
