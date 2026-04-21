package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/gateyes/gateway/internal/config"
)

func TestDriverNameSupportsKnownDrivers(t *testing.T) {
	tests := []struct {
		driver  string
		want    string
		wantErr bool
	}{
		{driver: "", want: "sqlite"},
		{driver: "sqlite", want: "sqlite"},
		{driver: "postgres", want: "postgres"},
		{driver: "mysql", want: "mysql"},
		{driver: "oracle", wantErr: true},
	}

	for _, tt := range tests {
		got, err := driverName(tt.driver)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("driverName(%q) error = nil, want non-nil", tt.driver)
			}
			continue
		}
		if err != nil {
			t.Fatalf("driverName(%q) error: %v", tt.driver, err)
		}
		if got != tt.want {
			t.Fatalf("driverName(%q) = %q, want %q", tt.driver, got, tt.want)
		}
	}
}

func TestOpenMigrateRebindAndClose(t *testing.T) {
	cfg := config.DatabaseConfig{
		Driver:                 "sqlite",
		DSN:                    filepath.Join(t.TempDir(), "db-test.sqlite"),
		AutoMigrate:            true,
		MaxOpenConns:           1,
		MaxIdleConns:           1,
		ConnMaxLifetimeSeconds: 60,
	}

	database, err := Open(cfg)
	if err != nil {
		t.Fatalf("Open(%+v) error: %v", cfg, err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})

	if got, want := database.Rebind("SELECT ?, ?"), "SELECT ?, ?"; got != want {
		t.Fatalf("DB.Rebind(sqlite) = %q, want %q", got, want)
	}

	pg := &DB{driver: "postgres"}
	if got, want := pg.Rebind("SELECT ?, ?, ?"), "SELECT $1, $2, $3"; got != want {
		t.Fatalf("DB.Rebind(postgres) = %q, want %q", got, want)
	}

	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("DB.Migrate() first error: %v", err)
	}
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("DB.Migrate() second error: %v", err)
	}

	var count int
	if err := database.Conn.QueryRowContext(context.Background(), `
SELECT COUNT(1)
FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("QueryRowContext(schema_migrations) error: %v", err)
	}
	if count == 0 {
		t.Fatalf("schema_migrations count = %d, want > 0", count)
	}

	if err := database.Close(); err != nil {
		t.Fatalf("DB.Close() error: %v", err)
	}
	if err := (*DB)(nil).Close(); err != nil {
		t.Fatalf("(*DB)(nil).Close() error: %v", err)
	}
}
