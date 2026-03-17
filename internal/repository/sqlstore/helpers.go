package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) loadModels(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(`
SELECT model
FROM user_models
WHERE user_id = ?
ORDER BY model`), userID)
	if err != nil {
		return nil, fmt.Errorf("load models: %w", err)
	}
	defer rows.Close()

	var models []string
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			return nil, fmt.Errorf("scan model: %w", err)
		}
		models = append(models, model)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate models: %w", err)
	}

	return models, nil
}

func (s *Store) replaceModels(ctx context.Context, tx *sql.Tx, userID string, models []string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, s.db.Rebind(`DELETE FROM user_models WHERE user_id = ?`), userID); err != nil {
		return fmt.Errorf("delete user models: %w", err)
	}

	for _, model := range models {
		if _, err := tx.ExecContext(ctx, s.db.Rebind(`
INSERT INTO user_models (id, user_id, model, created_at)
VALUES (?, ?, ?, ?)`), uuid.NewString(), userID, model, now); err != nil {
			return fmt.Errorf("insert user model: %w", err)
		}
	}

	return nil
}

func (s *Store) loadUser(ctx context.Context, tenantID string, idOrAPIKey string) (*repository.UserRecord, error) {
	query := `
SELECT u.id,
	u.tenant_id,
	t.slug,
	COALESCE((SELECT ak.key FROM api_keys ak WHERE ak.user_id = u.id ORDER BY ak.created_at LIMIT 1), ''),
	u.name,
	u.email,
	u.role,
	u.quota,
	u.used,
	u.qps,
	u.status,
	u.created_at,
	u.updated_at
FROM users u
JOIN tenants t ON t.id = u.tenant_id
WHERE (u.id = ? OR EXISTS (SELECT 1 FROM api_keys ak WHERE ak.user_id = u.id AND ak.key = ?))`

	args := []any{idOrAPIKey, idOrAPIKey}
	if tenantID != "" {
		query += `
  AND u.tenant_id = ?`
		args = append(args, tenantID)
	}
	query += `
LIMIT 1`

	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), args...)
	var user repository.UserRecord
	if err := row.Scan(
		&user.ID,
		&user.TenantID,
		&user.TenantSlug,
		&user.APIKey,
		&user.Name,
		&user.Email,
		&user.Role,
		&user.Quota,
		&user.Used,
		&user.QPS,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("load user: %w", err)
	}

	models, err := s.loadModels(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	user.Models = models

	return &user, nil
}

func (s *Store) loadTenant(ctx context.Context, idOrSlug string) (*repository.TenantRecord, error) {
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
SELECT id, slug, name, status, created_at, updated_at
FROM tenants
WHERE id = ?
   OR slug = ?
LIMIT 1`), idOrSlug, idOrSlug)

	var tenant repository.TenantRecord
	if err := row.Scan(
		&tenant.ID,
		&tenant.Slug,
		&tenant.Name,
		&tenant.Status,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("load tenant: %w", err)
	}

	return &tenant, nil
}
