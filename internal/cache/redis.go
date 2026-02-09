package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisCache implements a Redis-backed cache
type RedisCache struct {
	client *redis.Client
	stats  CacheStats
}

// NewRedisCache creates a new Redis cache
func NewRedisCache(addr, password string, db int) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &RedisCache{
		client: client,
	}, nil
}

// Get retrieves a value from Redis
func (rc *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	val, err := rc.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			rc.stats.Misses++
			return nil, ErrCacheMiss
		}
		return nil, err
	}

	rc.stats.Hits++
	return val, nil
}

// Set stores a value in Redis
func (rc *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	err := rc.client.Set(ctx, key, value, ttl).Err()
	if err != nil {
		return err
	}

	rc.stats.Sets++
	return nil
}

// Delete removes a value from Redis
func (rc *RedisCache) Delete(ctx context.Context, key string) error {
	err := rc.client.Del(ctx, key).Err()
	if err != nil {
		return err
	}

	rc.stats.Deletes++
	return nil
}

// Clear removes all values from Redis (use with caution!)
func (rc *RedisCache) Clear(ctx context.Context) error {
	return rc.client.FlushDB(ctx).Err()
}

// Stats returns cache statistics
func (rc *RedisCache) Stats() CacheStats {
	return rc.stats
}

// Close closes the Redis connection
func (rc *RedisCache) Close() error {
	return rc.client.Close()
}
