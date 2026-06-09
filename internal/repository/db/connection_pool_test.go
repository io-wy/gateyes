package db

import (
	"path/filepath"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
)

func TestOpen_ConnectionPoolConfig(t *testing.T) {
	cfg := config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "pool-test.sqlite"),
		MaxOpenConns:           7,
		MaxIdleConns:           3,
		ConnMaxLifetimeSeconds: 120,
	}

	db, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer db.Close()

	if got := db.Conn.Stats().MaxOpenConnections; got != 7 {
		t.Errorf("MaxOpenConns = %d, want 7", got)
	}
}
