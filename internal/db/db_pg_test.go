package db

import (
	"context"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestPostgresOpenAndMigrate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real postgres test in short mode")
	}

	cfg := config.DatabaseConfig{
		Driver:                 "postgres",
		DSN:                    "host=localhost port=5432 user=dev_user password=dev_pw_2026 dbname=dev_db sslmode=disable",
		MaxOpenConns:           4,
		MaxIdleConns:           2,
		ConnMaxLifetimeSeconds: 60,
	}

	database, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open(postgres) error: %v", err)
	}
	defer database.Close()

	t.Log("PostgreSQL connected")

	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate(postgres) error: %v", err)
	}

	t.Log("Migrations applied")

	var count int
	if err := database.Conn.QueryRowContext(context.Background(),
		"SELECT COUNT(1) FROM schema_migrations",
	).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count == 0 {
		t.Fatal("schema_migrations count = 0, want > 0")
	}
	t.Logf("Applied %d migrations", count)

	// Verify key tables exist
	tables := []string{"users", "api_keys", "virtual_keys", "responses", "tenants", "projects", "usage_records"}
	for _, table := range tables {
		var exists bool
		if err := database.Conn.QueryRowContext(context.Background(),
			"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", table,
		).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %q not found", table)
		}
	}
}

func TestMySQLOpenAndMigrate(t *testing.T) {
	t.Skip("TODO: MySQL migration files need SQLite→MySQL syntax translation")
	if testing.Short() {
		t.Skip("skipping real mysql test in short mode")
	}

	cfg := config.DatabaseConfig{
		Driver:                 "mysql",
		DSN:                    "dev_user:dev_pw_2026@tcp(127.0.0.1:3306)/dev_db?parseTime=true",
		MaxOpenConns:           4,
		MaxIdleConns:           2,
		ConnMaxLifetimeSeconds: 60,
	}

	database, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open(mysql) error: %v", err)
	}
	defer database.Close()

	t.Log("MySQL connected")

	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate(mysql) error: %v", err)
	}

	t.Log("Migrations applied")

	var count int
	if err := database.Conn.QueryRowContext(context.Background(),
		"SELECT COUNT(1) FROM schema_migrations",
	).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count == 0 {
		t.Fatal("schema_migrations count = 0, want > 0")
	}
	t.Logf("Applied %d migrations", count)

	// Verify key tables exist
	tables := []string{"users", "api_keys", "virtual_keys", "responses", "tenants", "projects", "usage_records"}
	for _, table := range tables {
		var count int
		if err := database.Conn.QueryRowContext(context.Background(),
			"SELECT COUNT(1) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", table,
		).Scan(&count); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if count == 0 {
			t.Errorf("table %q not found", table)
		}
	}
}
