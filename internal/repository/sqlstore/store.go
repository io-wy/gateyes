package sqlstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/db"
	"github.com/gateyes/gateway/internal/repository"
)

type Store struct {
	db *db.DB
}

func New(database *db.DB) *Store {
	return &Store{db: database}
}

func (s *Store) CreateUser(ctx context.Context, params repository.CreateUserParams) (*repository.UserRecord, error) {
	if _, err := s.loadTenant(ctx, params.TenantID); err != nil {
		return nil, err
	}
	if params.ProjectID != "" {
		if _, err := s.loadProject(ctx, params.TenantID, params.ProjectID); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	userID := uuid.NewString()
	apiKeyID := uuid.NewString()

	status := params.Status
	if status == "" {
		status = repository.StatusActive
	}
	role := params.Role
	if role == "" {
		role = repository.RoleTenantUser
	}

	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create user: %w", err)
	}

	if _, err := tx.ExecContext(ctx, s.db.Rebind(`
INSERT INTO users (id, tenant_id, name, email, role, status, quota, used, qps, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`),
		userID, params.TenantID, params.Name, params.Email, role, status, params.Quota, params.QPS, now, now,
	); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("insert user: %w", err)
	}

	if _, err := tx.ExecContext(ctx, s.db.Rebind(`
INSERT INTO api_keys (id, user_id, key, secret_hash, status, project_id, budget_usd, spent_usd, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`),
		apiKeyID, userID, params.APIKey, params.SecretHash, repository.StatusActive, params.ProjectID, params.KeyBudgetUSD, now, now,
	); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("insert api key: %w", err)
	}

	if err := s.replaceModels(ctx, tx, userID, params.Models, now); err != nil {
		tx.Rollback()
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create user: %w", err)
	}

	return s.GetUser(ctx, params.TenantID, userID)
}

func (s *Store) ListUsers(ctx context.Context, tenantID string) ([]repository.UserRecord, error) {
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
JOIN tenants t ON t.id = u.tenant_id`
	args := make([]any, 0, 1)
	if tenantID != "" {
		query += `
WHERE u.tenant_id = ?`
		args = append(args, tenantID)
	}
	query += `
ORDER BY u.created_at DESC`

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []repository.UserRecord
	for rows.Next() {
		var user repository.UserRecord
		if err := rows.Scan(
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
			return nil, fmt.Errorf("scan user: %w", err)
		}

		models, err := s.loadModels(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		user.Models = models
		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}

	return users, nil
}

func (s *Store) GetUser(ctx context.Context, tenantID string, idOrAPIKey string) (*repository.UserRecord, error) {
	return s.loadUser(ctx, tenantID, idOrAPIKey)
}

func (s *Store) UpdateUser(ctx context.Context, tenantID string, idOrAPIKey string, params repository.UpdateUserParams) (*repository.UserRecord, error) {
	user, err := s.GetUser(ctx, tenantID, idOrAPIKey)
	if err != nil {
		return nil, err
	}
	if params.ProjectID != nil && *params.ProjectID != "" {
		if _, err := s.loadProject(ctx, user.TenantID, *params.ProjectID); err != nil {
			return nil, err
		}
	}

	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update user: %w", err)
	}

	sets := make([]string, 0, 5)
	args := make([]any, 0, 6)

	if params.Role != nil {
		sets = append(sets, "role = ?")
		args = append(args, *params.Role)
	}
	if params.Quota != nil {
		sets = append(sets, "quota = ?")
		args = append(args, *params.Quota)
	}
	if params.QPS != nil {
		sets = append(sets, "qps = ?")
		args = append(args, *params.QPS)
	}
	if params.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *params.Status)
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC(), user.ID)

	if _, err := tx.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
UPDATE users
SET %s
WHERE id = ?`, strings.Join(sets, ", "))), args...); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("update user: %w", err)
	}

	keySets := make([]string, 0, 3)
	keyArgs := make([]any, 0, 4)
	if params.ProjectID != nil {
		keySets = append(keySets, "project_id = ?")
		keyArgs = append(keyArgs, *params.ProjectID)
	}
	if params.KeyBudgetUSD != nil {
		keySets = append(keySets, "budget_usd = ?")
		keyArgs = append(keyArgs, *params.KeyBudgetUSD)
	}
	if len(keySets) > 0 {
		keySets = append(keySets, "updated_at = ?")
		keyArgs = append(keyArgs, time.Now().UTC(), user.ID)
		if _, err := tx.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
UPDATE api_keys
SET %s
WHERE user_id = ?`, strings.Join(keySets, ", "))), keyArgs...); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("update user api key metadata: %w", err)
		}
	}

	if params.Models != nil {
		if err := s.replaceModels(ctx, tx, user.ID, *params.Models, time.Now().UTC()); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update user: %w", err)
	}

	return s.GetUser(ctx, user.TenantID, user.ID)
}

func (s *Store) DeleteUser(ctx context.Context, tenantID string, idOrAPIKey string) error {
	user, err := s.GetUser(ctx, tenantID, idOrAPIKey)
	if err != nil {
		return err
	}

	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete user: %w", err)
	}

	queries := []string{
		`DELETE FROM user_models WHERE user_id = ?`,
		`DELETE FROM usage_records WHERE user_id = ?`,
		`DELETE FROM responses WHERE user_id = ?`,
		`DELETE FROM api_keys WHERE user_id = ?`,
		`DELETE FROM users WHERE id = ?`,
	}

	for _, query := range queries {
		if _, err := tx.ExecContext(ctx, s.db.Rebind(query), user.ID); err != nil {
			tx.Rollback()
			return fmt.Errorf("delete user data: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete user: %w", err)
	}

	return nil
}

func (s *Store) ResetUserUsage(ctx context.Context, tenantID string, idOrAPIKey string) (*repository.UserRecord, error) {
	user, err := s.GetUser(ctx, tenantID, idOrAPIKey)
	if err != nil {
		return nil, err
	}

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE users
SET used = 0, updated_at = ?
WHERE id = ?`), time.Now().UTC(), user.ID); err != nil {
		return nil, fmt.Errorf("reset user usage: %w", err)
	}

	return s.GetUser(ctx, user.TenantID, user.ID)
}

func (s *Store) Stats(ctx context.Context, tenantID string) (*repository.UserStats, error) {
	stats := &repository.UserStats{}
	query := `
SELECT COUNT(1),
	COALESCE(SUM(CASE WHEN status = 'active' THEN 1 ELSE 0 END), 0),
	COALESCE(SUM(quota), 0),
	COALESCE(SUM(used), 0)
FROM users`
	args := make([]any, 0, 1)
	if tenantID != "" {
		query += `
WHERE tenant_id = ?`
		args = append(args, tenantID)
	}

	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), args...)
	if err := row.Scan(&stats.TotalUsers, &stats.ActiveUsers, &stats.TotalQuota, &stats.TotalUsed); err != nil {
		return nil, fmt.Errorf("user stats: %w", err)
	}
	return stats, nil
}

func (s *Store) EnsureTenant(ctx context.Context, params repository.EnsureTenantParams) (*repository.TenantRecord, error) {
	id := params.ID
	if id == "" {
		id = params.Slug
	}
	if id == "" {
		id = uuid.NewString()
	}
	slug := params.Slug
	if slug == "" {
		slug = id
	}
	name := params.Name
	if name == "" {
		name = slug
	}
	status := params.Status
	if status == "" {
		status = repository.StatusActive
	}

	tenant, err := s.loadTenant(ctx, id)
	if err != nil && slug != id {
		tenant, err = s.loadTenant(ctx, slug)
	}
	if err == nil {
		return tenant, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	now := time.Now().UTC()
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO tenants (id, slug, name, status, budget_usd, spent_usd, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, 0, ?, ?)`), id, slug, name, status, params.BudgetUSD, now, now); err != nil {
		return nil, fmt.Errorf("insert tenant: %w", err)
	}

	return s.loadTenant(ctx, id)
}
