package router

import (
	"gateyes/internal/cache"
	"gateyes/internal/config"
)

// convertCacheConfig converts config.CacheConfig to cache.CacheConfig
func convertCacheConfig(cfg config.CacheConfig) cache.CacheConfig {
	return cache.CacheConfig{
		Enabled:       cfg.Enabled,
		Backend:       cfg.Backend,
		TTL:           cfg.TTL.Duration,
		MaxSize:       cfg.MaxSize,
		MaxEntries:    cfg.MaxEntries,
		KeyStrategy:   cfg.KeyStrategy,
		RedisAddr:     cfg.RedisAddr,
		RedisPassword: cfg.RedisPassword,
		RedisDB:       cfg.RedisDB,
	}
}
