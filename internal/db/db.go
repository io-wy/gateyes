package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/gateyes/gateway/internal/config"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type DB struct {
	Conn   *sql.DB
	driver string
}

func Open(cfg config.DatabaseConfig) (*DB, error) {
	driverName, err := driverName(cfg.Driver)
	if err != nil {
		return nil, err
	}

	conn, err := sql.Open(driverName, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		conn.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		conn.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetimeSeconds > 0 {
		conn.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeSeconds) * time.Second)
	}

	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	if driverName == "sqlite" {
		if _, err := conn.Exec("PRAGMA journal_mode=WAL;"); err != nil {
			conn.Close()
			return nil, fmt.Errorf("enable sqlite wal mode: %w", err)
		}
		if _, err := conn.Exec("PRAGMA busy_timeout=5000;"); err != nil {
			conn.Close()
			return nil, fmt.Errorf("set sqlite busy timeout: %w", err)
		}
	}

	return &DB{Conn: conn, driver: cfg.Driver}, nil
}

func (d *DB) Close() error {
	if d == nil || d.Conn == nil {
		return nil
	}
	return d.Conn.Close()
}

func (d *DB) Rebind(query string) string {
	if d.driver != "postgres" {
		return query
	}

	var b strings.Builder
	b.Grow(len(query) + 8)

	arg := 1
	for _, ch := range query {
		if ch == '?' {
			b.WriteString(fmt.Sprintf("$%d", arg))
			arg++
			continue
		}
		b.WriteRune(ch)
	}

	return b.String()
}

func (d *DB) Migrate(ctx context.Context) error {
	versionCol := "TEXT"
	if d.driver == "mysql" {
		versionCol = "VARCHAR(255)"
	}
	if _, err := d.Conn.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS schema_migrations (version %s PRIMARY KEY, applied_at TIMESTAMP NOT NULL)`, versionCol)); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		applied, err := d.isApplied(ctx, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		data, err := migrationFS.ReadFile(filepath.ToSlash(filepath.Join("migrations", name)))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		migrationSQL := string(data)
		if d.driver == "mysql" {
			migrationSQL = mysqlCompatSQL(migrationSQL)
		}

		tx, err := d.Conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, migrationSQL); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, d.Rebind(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`), name, time.Now().UTC()); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}

	return nil
}

func (d *DB) isApplied(ctx context.Context, version string) (bool, error) {
	var count int
	if err := d.Conn.QueryRowContext(ctx, d.Rebind(`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`), version).Scan(&count); err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}
	return count > 0, nil
}

// mysqlCompatSQL adapts PostgreSQL/SQLite migration SQL for MySQL compatibility.
// Currently a minimal pass-through; extend as needed for MySQL-specific syntax.
func mysqlCompatSQL(sql string) string {
	return sql
}

func driverName(driver string) (string, error) {
	switch driver {
	case "", "sqlite":
		return "sqlite", nil
	case "postgres":
		return "pgx", nil
	default:
		return "", fmt.Errorf("unsupported database driver: %s", driver)
	}
}
