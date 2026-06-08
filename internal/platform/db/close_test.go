package db

import (
	"path/filepath"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
)

func TestClose_DoubleClose(t *testing.T) {
	cfg := config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "close-test.sqlite"),
		MaxOpenConns:           1,
		MaxIdleConns:           1,
		ConnMaxLifetimeSeconds: 60,
	}

	db, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("first Close error: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("second Close error: %v", err)
	}
}
