package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) CreateAPIKey(ctx context.Context, params repository.CreateAPIKeyParams) (*repository.APIKeyRecord, error) {
	user, err := s.loadUser(ctx, "", params.UserID)
	if err != nil {
		return nil, err
	}
	if params.ProjectID != "" {
		if _, err := s.loadProject(ctx, user.TenantID, params.ProjectID); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	status := params.Status
	if status == "" {
		status = repository.StatusActive
	}

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO api_keys (
	id, user_id, key, secret_hash, name, status, project_id, budget_usd, spent_usd,
	allowed_models, allowed_providers, allowed_services, rate_limit_qps, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?)`),
		uuid.NewString(),
		user.ID,
		params.Key,
		params.SecretHash,
		params.Key,
		status,
		params.ProjectID,
		params.BudgetUSD,
		encodeStringSlice(params.AllowedModels),
		encodeStringSlice(params.AllowedProviders),
		encodeStringSlice(params.AllowedServices),
		params.RateLimitQPS,
		now,
		now,
	); err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}

	return s.GetAPIKey(ctx, user.TenantID, params.Key)
}

func (s *Store) ListAPIKeys(ctx context.Context, tenantID string, filter repository.APIKeyFilter) ([]repository.APIKeyRecord, error) {
	query := `
SELECT ak.id, u.tenant_id, t.slug, ak.user_id, u.name, u.email,
	COALESCE(ak.project_id, ''), COALESCE(p.slug, ''), ak.key, ak.status,
	ak.budget_usd, ak.spent_usd, ak.rate_limit_qps, ak.allowed_models, ak.allowed_providers, ak.allowed_services,
	ak.last_used_at, ak.revoked_at, ak.created_at, ak.updated_at
FROM api_keys ak
JOIN users u ON u.id = ak.user_id
JOIN tenants t ON t.id = u.tenant_id
LEFT JOIN projects p ON p.id = ak.project_id
WHERE 1 = 1`

	args := make([]any, 0, 4)
	if tenantID != "" {
		query += ` AND u.tenant_id = ?`
		args = append(args, tenantID)
	}
	if filter.UserID != "" {
		query += ` AND ak.user_id = ?`
		args = append(args, filter.UserID)
	}
	if filter.ProjectID != "" {
		query += ` AND ak.project_id = ?`
		args = append(args, filter.ProjectID)
	}
	if filter.Status != "" {
		query += ` AND ak.status = ?`
		args = append(args, filter.Status)
	}
	query += ` ORDER BY ak.created_at DESC`

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var items []repository.APIKeyRecord
	for rows.Next() {
		record, err := scanAPIKeyRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate api keys: %w", err)
	}
	return items, nil
}

func (s *Store) GetAPIKey(ctx context.Context, tenantID string, idOrKey string) (*repository.APIKeyRecord, error) {
	query := `
SELECT ak.id, u.tenant_id, t.slug, ak.user_id, u.name, u.email,
	COALESCE(ak.project_id, ''), COALESCE(p.slug, ''), ak.key, ak.status,
	ak.budget_usd, ak.spent_usd, ak.rate_limit_qps, ak.allowed_models, ak.allowed_providers, ak.allowed_services,
	ak.last_used_at, ak.revoked_at, ak.created_at, ak.updated_at
FROM api_keys ak
JOIN users u ON u.id = ak.user_id
JOIN tenants t ON t.id = u.tenant_id
LEFT JOIN projects p ON p.id = ak.project_id
WHERE (ak.id = ? OR ak.key = ?)`
	args := []any{idOrKey, idOrKey}
	if tenantID != "" {
		query += ` AND u.tenant_id = ?`
		args = append(args, tenantID)
	}
	query += ` LIMIT 1`

	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), args...)
	record, err := scanAPIKeyRecord(row)
	if err == sql.ErrNoRows {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return record, nil
}

func (s *Store) UpdateAPIKey(ctx context.Context, tenantID string, idOrKey string, params repository.UpdateAPIKeyParams) (*repository.APIKeyRecord, error) {
	record, err := s.GetAPIKey(ctx, tenantID, idOrKey)
	if err != nil {
		return nil, err
	}
	if params.ProjectID != nil && *params.ProjectID != "" {
		if _, err := s.loadProject(ctx, record.TenantID, *params.ProjectID); err != nil {
			return nil, err
		}
	}

	sets := make([]string, 0, 8)
	args := make([]any, 0, 9)
	if params.ProjectID != nil {
		sets = append(sets, "project_id = ?")
		args = append(args, *params.ProjectID)
	}
	if params.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *params.Status)
	}
	if params.BudgetUSD != nil {
		sets = append(sets, "budget_usd = ?")
		args = append(args, *params.BudgetUSD)
	}
	if params.RateLimitQPS != nil {
		sets = append(sets, "rate_limit_qps = ?")
		args = append(args, *params.RateLimitQPS)
	}
	if params.AllowedModels != nil {
		sets = append(sets, "allowed_models = ?")
		args = append(args, encodeStringSlice(*params.AllowedModels))
	}
	if params.AllowedProviders != nil {
		sets = append(sets, "allowed_providers = ?")
		args = append(args, encodeStringSlice(*params.AllowedProviders))
	}
	if params.AllowedServices != nil {
		sets = append(sets, "allowed_services = ?")
		args = append(args, encodeStringSlice(*params.AllowedServices))
	}
	if params.RevokedAt != nil {
		sets = append(sets, "revoked_at = ?")
		args = append(args, *params.RevokedAt)
	} else if params.Status != nil && *params.Status == repository.StatusRevoked {
		now := time.Now().UTC()
		sets = append(sets, "revoked_at = ?")
		args = append(args, now)
	} else if params.Status != nil && *params.Status == repository.StatusActive {
		sets = append(sets, "revoked_at = NULL")
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC(), record.ID)

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
UPDATE api_keys
SET %s
WHERE id = ?`, strings.Join(sets, ", "))), args...); err != nil {
		return nil, fmt.Errorf("update api key: %w", err)
	}

	return s.GetAPIKey(ctx, record.TenantID, record.ID)
}

func (s *Store) RotateAPIKey(ctx context.Context, tenantID string, idOrKey string, params repository.RotateAPIKeyParams) (*repository.APIKeyRecord, error) {
	record, err := s.GetAPIKey(ctx, tenantID, idOrKey)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE api_keys
SET key = ?, secret_hash = ?, status = ?, revoked_at = NULL, updated_at = ?
WHERE id = ?`), params.NewKey, params.NewSecretHash, repository.StatusActive, time.Now().UTC(), record.ID); err != nil {
		return nil, fmt.Errorf("rotate api key: %w", err)
	}
	return s.GetAPIKey(ctx, record.TenantID, record.ID)
}

func scanAPIKeyRecord(scanner rowScanner) (*repository.APIKeyRecord, error) {
	record := &repository.APIKeyRecord{}
	var allowedModelsRaw string
	var allowedProvidersRaw string
	var allowedServicesRaw string
	var lastUsedAt sql.NullTime
	var revokedAt sql.NullTime
	if err := scanner.Scan(
		&record.ID,
		&record.TenantID,
		&record.TenantSlug,
		&record.UserID,
		&record.UserName,
		&record.UserEmail,
		&record.ProjectID,
		&record.ProjectSlug,
		&record.Key,
		&record.Status,
		&record.BudgetUSD,
		&record.SpentUSD,
		&record.RateLimitQPS,
		&allowedModelsRaw,
		&allowedProvidersRaw,
		&allowedServicesRaw,
		&lastUsedAt,
		&revokedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	record.AllowedModels = decodeStringSlice(allowedModelsRaw)
	record.AllowedProviders = decodeStringSlice(allowedProvidersRaw)
	record.AllowedServices = decodeStringSlice(allowedServicesRaw)
	if lastUsedAt.Valid {
		value := lastUsedAt.Time
		record.LastUsedAt = &value
	}
	if revokedAt.Valid {
		value := revokedAt.Time
		record.RevokedAt = &value
	}
	return record, nil
}
