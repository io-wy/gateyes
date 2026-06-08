package redis

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
)

func TestNewClient_Config(t *testing.T) {
	cfg := config.RedisConfig{
		Addr:     "localhost:6379",
		Password: "secret",
		DB:       2,
		PoolSize: 20,
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	if client == nil {
		t.Fatal("NewClient() returned nil client")
	}
	Close(client)
}

func TestNewClient_DefaultPoolSize(t *testing.T) {
	cfg := config.RedisConfig{
		Addr: "localhost:6379",
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	if client == nil {
		t.Fatal("NewClient() returned nil client")
	}
	Close(client)
}

func TestClose_NilClient(t *testing.T) {
	if err := Close(nil); err != nil {
		t.Fatalf("Close(nil) error: %v", err)
	}
}

func TestPing_NilClient(t *testing.T) {
	if err := Ping(nil); err == nil {
		t.Error("Ping(nil) should return error")
	}
}

func TestPing_RealRedis(t *testing.T) {
	cfg := config.RedisConfig{
		Addr:     "127.0.0.1:6379",
		Password: "dev_redis_pw_2026",
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	defer Close(client)

	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}
}
