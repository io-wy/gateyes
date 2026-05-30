package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) CreateVirtualKey(ctx context.Context, params repository.CreateVirtualKeyParams) (*repository.VirtualKeyRecord, error) {
	id := uuid.NewString()
	now := time.Now().UTC()
	modelsJSON, err := json.Marshal(params.AllowedModels)
	if err != nil {
		return nil, fmt.Errorf("marshal allowed_models: %w", err)
	}
	providersJSON, err := json.Marshal(params.AllowedProviders)
	if err != nil {
		return nil, fmt.Errorf("marshal allowed_providers: %w", err)
	}
	metadataJSON, err := json.Marshal(params.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO virtual_keys (
	id, tenant_id, project_id, user_id, api_key_id, name, key, secret_hash,
	status, budget_usd, budget_policy, rate_limit_qps, allowed_models, allowed_providers,
	metadata, callback_url, expires_at, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		id, params.TenantID, params.ProjectID, params.UserID, params.APIKeyID, params.Name, params.Key, params.SecretHash,
		repository.StatusActive, params.BudgetUSD, params.BudgetPolicy, params.RateLimitQPS, string(modelsJSON), string(providersJSON),
		string(metadataJSON), params.CallbackURL, params.ExpiresAt, now, now,
	); err != nil {
		return nil, fmt.Errorf("create virtual key: %w", err)
	}
	return s.GetVirtualKey(ctx, params.TenantID, id)
}

func (s *Store) ListVirtualKeys(ctx context.Context, tenantID string, filter repository.VirtualKeyFilter) ([]repository.VirtualKeyRecord, error) {
	query := `
SELECT id, tenant_id, project_id, user_id, api_key_id, name, key, secret_hash,
	status, budget_usd, spent_usd, budget_policy, rate_limit_qps, allowed_models, allowed_providers,
	metadata, callback_url, expires_at, revoked_at, created_at, updated_at
FROM virtual_keys
WHERE tenant_id = ?`
	args := []any{tenantID}
	if filter.UserID != "" {
		query += ` AND user_id = ?`
		args = append(args, filter.UserID)
	}
	if filter.ProjectID != "" {
		query += ` AND project_id = ?`
		args = append(args, filter.ProjectID)
	}
	if filter.APIKeyID != "" {
		query += ` AND api_key_id = ?`
		args = append(args, filter.APIKeyID)
	}
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list virtual keys: %w", err)
	}
	defer rows.Close()

	var items []repository.VirtualKeyRecord
	for rows.Next() {
		record, err := scanVirtualKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate virtual keys: %w", err)
	}
	return items, nil
}

func (s *Store) GetVirtualKey(ctx context.Context, tenantID string, idOrKey string) (*repository.VirtualKeyRecord, error) {
	query := `
SELECT id, tenant_id, project_id, user_id, api_key_id, name, key, secret_hash,
	status, budget_usd, spent_usd, budget_policy, rate_limit_qps, allowed_models, allowed_providers,
	metadata, callback_url, expires_at, revoked_at, created_at, updated_at
FROM virtual_keys
WHERE (id = ? OR key = ?)`
	args := []any{idOrKey, idOrKey}
	if tenantID != "" {
		query += ` AND tenant_id = ?`
		args = append(args, tenantID)
	}
	query += ` LIMIT 1`

	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), args...)
	record, err := scanVirtualKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("get virtual key: %w", err)
	}
	return &record, nil
}

func (s *Store) UpdateVirtualKey(ctx context.Context, tenantID string, idOrKey string, params repository.UpdateVirtualKeyParams) (*repository.VirtualKeyRecord, error) {
	record, err := s.GetVirtualKey(ctx, tenantID, idOrKey)
	if err != nil {
		return nil, err
	}

	setClauses := []string{}
	args := []any{}
	if params.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *params.Name)
	}
	if params.Status != nil {
		setClauses = append(setClauses, "status = ?")
		args = append(args, *params.Status)
	}
	if params.BudgetUSD != nil {
		setClauses = append(setClauses, "budget_usd = ?")
		args = append(args, *params.BudgetUSD)
	}
	if params.BudgetPolicy != nil {
		setClauses = append(setClauses, "budget_policy = ?")
		args = append(args, *params.BudgetPolicy)
	}
	if params.RateLimitQPS != nil {
		setClauses = append(setClauses, "rate_limit_qps = ?")
		args = append(args, *params.RateLimitQPS)
	}
	if params.AllowedModels != nil {
		setClauses = append(setClauses, "allowed_models = ?")
		v := mustJSON(*params.AllowedModels)
		if err := validateJSON("allowed_models", v); err != nil {
			return nil, err
		}
		args = append(args, v)
	}
	if params.AllowedProviders != nil {
		setClauses = append(setClauses, "allowed_providers = ?")
		v := mustJSON(*params.AllowedProviders)
		if err := validateJSON("allowed_providers", v); err != nil {
			return nil, err
		}
		args = append(args, v)
	}
	if params.Metadata != nil {
		setClauses = append(setClauses, "metadata = ?")
		v := mustJSON(*params.Metadata)
		if err := validateJSON("metadata", v); err != nil {
			return nil, err
		}
		args = append(args, v)
	}
	if params.CallbackURL != nil {
		setClauses = append(setClauses, "callback_url = ?")
		args = append(args, *params.CallbackURL)
	}
	if params.ExpiresAt != nil {
		setClauses = append(setClauses, "expires_at = ?")
		args = append(args, *params.ExpiresAt)
	}
	if params.RevokedAt != nil {
		setClauses = append(setClauses, "revoked_at = ?")
		args = append(args, *params.RevokedAt)
	}
	if len(setClauses) == 0 {
		return record, nil
	}

	setClauses = append(setClauses, "updated_at = ?")
	args = append(args, time.Now().UTC())
	args = append(args, record.ID)

	query := "UPDATE virtual_keys SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(query), args...); err != nil {
		return nil, fmt.Errorf("update virtual key: %w", err)
	}
	return s.GetVirtualKey(ctx, tenantID, record.ID)
}

func (s *Store) DeleteVirtualKey(ctx context.Context, tenantID string, idOrKey string) error {
	record, err := s.GetVirtualKey(ctx, tenantID, idOrKey)
	if err != nil {
		return err
	}
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`DELETE FROM virtual_keys WHERE id = ?`), record.ID); err != nil {
		return fmt.Errorf("delete virtual key: %w", err)
	}
	return nil
}

func (s *Store) AuthenticateVirtualKey(ctx context.Context, key string) (*repository.VirtualKeyRecord, error) {
	query := `
SELECT id, tenant_id, project_id, user_id, api_key_id, name, key, secret_hash,
	status, budget_usd, spent_usd, budget_policy, rate_limit_qps, allowed_models, allowed_providers,
	metadata, callback_url, expires_at, revoked_at, created_at, updated_at
FROM virtual_keys
WHERE key = ? AND status = ?`
	args := []any{key, repository.StatusActive}

	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), args...)
	record, err := scanVirtualKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("authenticate virtual key: %w", err)
	}
	if record.ExpiresAt != nil && record.ExpiresAt.Before(time.Now().UTC()) {
		return nil, repository.ErrNotFound
	}
	return &record, nil
}

type virtualKeyScanner interface {
	Scan(dest ...any) error
}

func scanVirtualKey(scanner virtualKeyScanner) (repository.VirtualKeyRecord, error) {
	var record repository.VirtualKeyRecord
	var modelsJSON string
	var providersJSON string
	var metadataJSON string
	err := scanner.Scan(
		&record.ID, &record.TenantID, &record.ProjectID, &record.UserID, &record.APIKeyID,
		&record.Name, &record.Key, &record.SecretHash, &record.Status,
		&record.BudgetUSD, &record.SpentUSD, &record.BudgetPolicy, &record.RateLimitQPS,
		&modelsJSON, &providersJSON, &metadataJSON, &record.CallbackURL,
		&record.ExpiresAt, &record.RevokedAt, &record.CreatedAt, &record.UpdatedAt,
	)
	if err != nil {
		return record, err
	}
	_ = json.Unmarshal([]byte(modelsJSON), &record.AllowedModels)
	_ = json.Unmarshal([]byte(providersJSON), &record.AllowedProviders)
	_ = json.Unmarshal([]byte(metadataJSON), &record.Metadata)
	return record, nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
