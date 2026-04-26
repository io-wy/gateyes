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

func redisTryConsume(rdb *redis.Client, redisKey string, n, rate, burst int) bool {
	now := time.Now().UnixMilli()
	result, err := tokenBucketLua.Run(context.Background(), rdb, []string{redisKey}, rate, burst, n, now).Int()
	if err != nil {
		return true
	}
	return result == 1
}

func limiterKey(parts ...string) string {
	return fmt.Sprintf("gateyes:rl:%s", joinKey(parts))
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
