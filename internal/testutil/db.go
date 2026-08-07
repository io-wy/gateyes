package testutil

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gateyes/gateway/internal/app/config"
	"github.com/gateyes/gateway/internal/repository/db"
	"github.com/redis/go-redis/v9"
)

// OpenTestDB creates an in-memory SQLite database, runs migrations, and returns it.
func OpenTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	database.Conn.SetMaxOpenConns(1)
	database.Conn.SetMaxIdleConns(1)
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// NewRedisClient creates a miniredis instance and returns a redis client connected to it.
func NewRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}
