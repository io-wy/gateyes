package redis

import (
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	Addr           string
	Password       string
	DB             int
	MinIdleConns   int
	MaxRetries     int
	PoolSize       int
	DialTimeoutMs  int
	ReadTimeoutMs  int
	WriteTimeoutMs int
}

func NewClient(cfg Config) (*redis.Client, error) {
	opts := &redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		MinIdleConns: cfg.MinIdleConns,
		MaxRetries:   cfg.MaxRetries,
	}
	if cfg.PoolSize > 0 {
		opts.PoolSize = cfg.PoolSize
	}
	if cfg.DialTimeoutMs > 0 {
		opts.DialTimeout = time.Duration(cfg.DialTimeoutMs) * time.Millisecond
	}
	if cfg.ReadTimeoutMs > 0 {
		opts.ReadTimeout = time.Duration(cfg.ReadTimeoutMs) * time.Millisecond
	}
	if cfg.WriteTimeoutMs > 0 {
		opts.WriteTimeout = time.Duration(cfg.WriteTimeoutMs) * time.Millisecond
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
