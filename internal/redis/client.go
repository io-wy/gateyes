package redis

import (
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/gateyes/gateway/internal/config"
)

func NewClient(cfg config.RedisConfig) (*redis.Client, error) {
	opts := &redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	}
	if cfg.PoolSize > 0 {
		opts.PoolSize = cfg.PoolSize
	}
	client := redis.NewClient(opts)
	return client, nil
}

func Close(client *redis.Client) error {
	if client == nil {
		return nil
	}
	return client.Close()
}

func Ping(client *redis.Client) error {
	if client == nil {
		return fmt.Errorf("redis client is nil")
	}
	return nil
}
