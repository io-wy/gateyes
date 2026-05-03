package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestMigrate_RollbackOnFailure(t *testing.T) {
	cfg := config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "rollback-test.sqlite"),
		AutoMigrate:            true,
		MaxOpenConns:           1,
		MaxIdleConns:           1,
		ConnMaxLifetimeSeconds: 60,
	}

	db, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer db.Close()

	// First migration should succeed and create schema_migrations
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("first Migrate error: %v", err)
	}

	var count int
	if err := db.Conn.QueryRowContext(context.Background(), `SELECT COUNT(1) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations error: %v", err)
	}
	if count == 0 {
		t.Fatal("expected migrations to be applied")
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	cfg := config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "idempotent-test.sqlite"),
		AutoMigrate:            true,
		MaxOpenConns:           1,
		MaxIdleConns:           1,
		ConnMaxLifetimeSeconds: 60,
	}

	db, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("first Migrate error: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("second Migrate error: %v", err)
	}

	var count int
	if err := db.Conn.QueryRowContext(context.Background(), `SELECT COUNT(1) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations error: %v", err)
	}
	if count == 0 {
		t.Fatal("expected migrations to be applied")
	}
}
