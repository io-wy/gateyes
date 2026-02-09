package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Cache defines the interface for response caching
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Clear(ctx context.Context) error
	Stats() CacheStats
}

// CacheStats represents cache statistics
type CacheStats struct {
	Hits        int64
	Misses      int64
	Sets        int64
	Deletes     int64
	Evictions   int64
	Size        int64
	HitRate     float64
}

// CacheConfig defines cache configuration
type CacheConfig struct {
	Enabled       bool
	Backend       string        // "memory", "redis"
	TTL           time.Duration
	MaxSize       int64         // Max cache size in bytes (for memory cache)
	MaxEntries    int           // Max number of entries (for memory cache)
	KeyStrategy   string        // "full", "semantic"
	RedisAddr     string
	RedisPassword string
	RedisDB       int
}

// CacheKey represents a cache key with metadata
type CacheKey struct {
	Provider string
	Model    string
	Messages string
	Hash     string
}

// CacheEntry represents a cached response
type CacheEntry struct {
	Key       string
	Value     []byte
	CreatedAt time.Time
	ExpiresAt time.Time
	Size      int64
	Metadata  map[string]interface{}
}

// CacheManager manages response caching
type CacheManager struct {
	config CacheConfig
	cache  Cache
	mu     sync.RWMutex
}

// NewCacheManager creates a new cache manager
func NewCacheManager(config CacheConfig) (*CacheManager, error) {
	if !config.Enabled {
		return &CacheManager{
			config: config,
			cache:  NewNoOpCache(),
		}, nil
	}

	var cache Cache
	var err error

	switch config.Backend {
	case "memory":
		cache = NewMemoryCache(config.MaxSize, config.MaxEntries)
	case "redis":
		cache, err = NewRedisCache(config.RedisAddr, config.RedisPassword, config.RedisDB)
		if err != nil {
			return nil, fmt.Errorf("failed to create redis cache: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported cache backend: %s", config.Backend)
	}

	slog.Info("cache manager initialized",
		"backend", config.Backend,
		"ttl", config.TTL,
		"key_strategy", config.KeyStrategy,
	)

	return &CacheManager{
		config: config,
		cache:  cache,
	}, nil
}

// GenerateKey generates a cache key from request parameters
func (cm *CacheManager) GenerateKey(provider, model string, messages interface{}) (string, error) {
	// Create cache key structure
	key := CacheKey{
		Provider: provider,
		Model:    model,
	}

	// Serialize messages
	messagesJSON, err := json.Marshal(messages)
	if err != nil {
		return "", fmt.Errorf("failed to marshal messages: %w", err)
	}
	key.Messages = string(messagesJSON)

	// Generate hash
	hash := sha256.New()
	hash.Write([]byte(key.Provider))
	hash.Write([]byte(key.Model))
	hash.Write(messagesJSON)
	key.Hash = hex.EncodeToString(hash.Sum(nil))

	return key.Hash, nil
}

// Get retrieves a cached response
func (cm *CacheManager) Get(ctx context.Context, key string) ([]byte, bool, error) {
	if !cm.config.Enabled {
		return nil, false, nil
	}

	value, err := cm.cache.Get(ctx, key)
	if err != nil {
		if errors.Is(err, ErrCacheMiss) {
			return nil, false, nil
		}
		return nil, false, err
	}

	slog.Debug("cache hit", "key", key[:16]+"...")
	return value, true, nil
}

// Set stores a response in cache
func (cm *CacheManager) Set(ctx context.Context, key string, value []byte) error {
	if !cm.config.Enabled {
		return nil
	}

	ttl := cm.config.TTL
	if ttl == 0 {
		ttl = 1 * time.Hour // Default TTL
	}

	err := cm.cache.Set(ctx, key, value, ttl)
	if err != nil {
		slog.Error("failed to set cache", "error", err)
		return err
	}

	slog.Debug("cache set", "key", key[:16]+"...", "size", len(value))
	return nil
}

// Delete removes a cached response
func (cm *CacheManager) Delete(ctx context.Context, key string) error {
	if !cm.config.Enabled {
		return nil
	}

	return cm.cache.Delete(ctx, key)
}

// Clear clears all cached responses
func (cm *CacheManager) Clear(ctx context.Context) error {
	if !cm.config.Enabled {
		return nil
	}

	return cm.cache.Clear(ctx)
}

// GetStats returns cache statistics
func (cm *CacheManager) GetStats() CacheStats {
	if !cm.config.Enabled {
		return CacheStats{}
	}

	stats := cm.cache.Stats()

	// Calculate hit rate
	total := stats.Hits + stats.Misses
	if total > 0 {
		stats.HitRate = float64(stats.Hits) / float64(total)
	}

	return stats
}

// Close cleans up cache resources
func (cm *CacheManager) Close() error {
	if closer, ok := cm.cache.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// Common errors
var (
	ErrCacheMiss     = errors.New("cache miss")
	ErrCacheFull     = errors.New("cache full")
	ErrInvalidKey    = errors.New("invalid cache key")
	ErrExpired       = errors.New("cache entry expired")
)
