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

func (s *Store) Authenticate(ctx context.Context, key string) (*repository.AuthIdentity, error) {
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
SELECT ak.id, ak.key, ak.secret_hash, ak.status,
	u.id, u.name, u.email, u.status, u.quota, u.used, u.qps, u.role,
	t.id, t.slug, t.status
FROM api_keys ak
JOIN users u ON u.id = ak.user_id
JOIN tenants t ON t.id = u.tenant_id
WHERE ak.key = ?
LIMIT 1`), key)

	identity := &repository.AuthIdentity{}
	if err := row.Scan(
		&identity.APIKeyID,
		&identity.APIKey,
		&identity.SecretHash,
		&identity.APIStatus,
		&identity.UserID,
		&identity.UserName,
		&identity.UserEmail,
		&identity.UserStatus,
		&identity.Quota,
		&identity.Used,
		&identity.QPS,
		&identity.Role,
		&identity.TenantID,
		&identity.TenantSlug,
		&identity.TenantStatus,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("authenticate key: %w", err)
	}

	models, err := s.loadModels(ctx, identity.UserID)
	if err != nil {
		return nil, err
	}
	identity.Models = models

	return identity, nil
}

func (s *Store) TouchAPIKey(ctx context.Context, apiKeyID string, at time.Time) error {
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE api_keys
SET last_used_at = ?, updated_at = ?
WHERE id = ?`), at, at, apiKeyID); err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return nil
}

func (s *Store) ConsumeQuota(ctx context.Context, userID string, tokens int) (bool, error) {
	result, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE users
SET used = used + ?, updated_at = ?
WHERE id = ?
  AND (quota <= 0 OR used + ? <= quota)`),
		tokens,
		time.Now().UTC(),
		userID,
		tokens,
	)
	if err != nil {
		return false, fmt.Errorf("consume quota: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("consume quota rows affected: %w", err)
	}

	return rowsAffected > 0, nil
}

func (s *Store) EnsureBootstrapKey(ctx context.Context, params repository.BootstrapAPIKeyParams) error {
	if _, err := s.loadTenant(ctx, params.TenantID); err != nil {
		return err
	}

	existing, err := s.Authenticate(ctx, params.Key)
	if err == nil {
		tx, err := s.db.Conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin bootstrap update: %w", err)
		}

		if _, err := tx.ExecContext(ctx, s.db.Rebind(`
UPDATE users
SET tenant_id = ?, name = ?, email = ?, role = ?, quota = ?, qps = ?, status = ?, updated_at = ?
WHERE id = ?`),
			params.TenantID,
			params.Name,
			params.Email,
			defaultRole(params.Role),
			params.Quota,
			params.QPS,
			repository.StatusActive,
			time.Now().UTC(),
			existing.UserID,
		); err != nil {
			tx.Rollback()
			return fmt.Errorf("update bootstrap user: %w", err)
		}

		if _, err := tx.ExecContext(ctx, s.db.Rebind(`
UPDATE api_keys
SET secret_hash = ?, status = ?, updated_at = ?
WHERE id = ?`), params.SecretHash, repository.StatusActive, time.Now().UTC(), existing.APIKeyID); err != nil {
			tx.Rollback()
			return fmt.Errorf("update bootstrap key: %w", err)
		}

		if err := s.replaceModels(ctx, tx, existing.UserID, params.Models, time.Now().UTC()); err != nil {
			tx.Rollback()
			return err
		}

		return tx.Commit()
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return err
	}

	now := time.Now().UTC()
	userID := uuid.NewString()
	apiKeyID := uuid.NewString()

	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bootstrap create: %w", err)
	}

	if _, err := tx.ExecContext(ctx, s.db.Rebind(`
INSERT INTO users (id, tenant_id, name, email, role, status, quota, used, qps, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`),
		userID,
		params.TenantID,
		params.Name,
		params.Email,
		defaultRole(params.Role),
		repository.StatusActive,
		params.Quota,
		params.QPS,
		now,
		now,
	); err != nil {
		tx.Rollback()
		return fmt.Errorf("insert bootstrap user: %w", err)
	}

	if _, err := tx.ExecContext(ctx, s.db.Rebind(`
INSERT INTO api_keys (id, user_id, key, secret_hash, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`),
		apiKeyID,
		userID,
		params.Key,
		params.SecretHash,
		repository.StatusActive,
		now,
		now,
	); err != nil {
		tx.Rollback()
		return fmt.Errorf("insert bootstrap key: %w", err)
	}

	if err := s.replaceModels(ctx, tx, userID, params.Models, now); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func defaultRole(role string) string {
	if role == "" {
		return repository.RoleTenantUser
	}
	return role
}
