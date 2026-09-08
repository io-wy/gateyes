package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gateyes/gateway/internal/app/config"
)

func TestDriverNameSupportsKnownDrivers(t *testing.T) {
	tests := []struct {
		driver  string
		want    string
		wantErr bool
	}{
		{driver: "", want: "sqlite"},
		{driver: "sqlite", want: "sqlite"},
		{driver: "postgres", want: "pgx"},
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

func TestMigrationNamesUseTableNamedOrder(t *testing.T) {
	names, err := migrationNames()
	if err != nil {
		t.Fatalf("migrationNames() error: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("migrationNames() returned no migrations")
	}
	for _, name := range names {
		if len(name) >= 4 && name[0] >= '0' && name[0] <= '9' && strings.Contains(name[:4], "_") {
			t.Fatalf("migration %q still uses a version prefix", name)
		}
	}
	for i, want := range migrationOrder {
		if i >= len(names) {
			t.Fatalf("migrationNames() length = %d, want at least %d", len(names), len(migrationOrder))
		}
		if got := names[i]; got != want {
			t.Fatalf("migrationNames()[%d] = %q, want %q", i, got, want)
		}
	}
	foundSemantic := false
	for _, name := range names {
		if name == "semantic_cache_entries.sql" {
			foundSemantic = true
			break
		}
	}
	if !foundSemantic {
		t.Fatal("migrationNames() missing semantic_cache_entries.sql")
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

	var semanticTables int
	if err := database.Conn.QueryRowContext(context.Background(), `
SELECT COUNT(1)
FROM sqlite_master
WHERE type = 'table' AND name = 'semantic_cache_entries'`).Scan(&semanticTables); err != nil {
		t.Fatalf("QueryRowContext(semantic_cache_entries) error: %v", err)
	}
	if semanticTables != 1 {
		t.Fatalf("semantic_cache_entries table count = %d, want 1", semanticTables)
	}

	pending, err := database.MigrationStatus(context.Background())
	if err != nil {
		t.Fatalf("DB.MigrationStatus() after migrate error: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("DB.MigrationStatus() after migrate = %v, want none", pending)
	}

	freshCfg := config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    filepath.Join(t.TempDir(), "db-pending.sqlite"),
	}
	freshDB, err := Open(freshCfg)
	if err != nil {
		t.Fatalf("Open(fresh) error: %v", err)
	}
	defer freshDB.Close()

	pending, err = freshDB.MigrationStatus(context.Background())
	if err != nil {
		t.Fatalf("DB.MigrationStatus() fresh error: %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("DB.MigrationStatus() fresh returned no pending migrations")
	}

	var freshCount int
	if err := freshDB.Conn.QueryRowContext(context.Background(), `SELECT COUNT(1) FROM schema_migrations`).Scan(&freshCount); err != nil {
		t.Fatalf("QueryRowContext(fresh schema_migrations) error: %v", err)
	}
	if freshCount != 0 {
		t.Fatalf("fresh schema_migrations count = %d, want 0", freshCount)
	}

	if err := database.Close(); err != nil {
		t.Fatalf("DB.Close() error: %v", err)
	}
	if err := (*DB)(nil).Close(); err != nil {
		t.Fatalf("(*DB)(nil).Close() error: %v", err)
	}
}

func TestCompatSQLRewritesPgvectorDDL(t *testing.T) {
	in := `CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE semantic_cache_entries (embedding vector(1536) NOT NULL, stream_body BYTEA);
CREATE INDEX semantic_cache_lookup_idx ON semantic_cache_entries USING hnsw (embedding vector_cosine_ops);`

	sqlite := sqliteCompatSQL(in)
	if strings.Contains(sqlite, "CREATE EXTENSION") || strings.Contains(sqlite, "vector(") || strings.Contains(sqlite, "USING hnsw") || strings.Contains(sqlite, "BYTEA") {
		t.Fatalf("sqliteCompatSQL() = %q, still contains pgvector syntax", sqlite)
	}

	mysql := mysqlCompatSQL(in)
	if strings.Contains(mysql, "CREATE EXTENSION") || strings.Contains(mysql, "vector(") || strings.Contains(mysql, "USING hnsw") || strings.Contains(mysql, "BYTEA") {
		t.Fatalf("mysqlCompatSQL() = %q, still contains pgvector syntax", mysql)
	}
}
