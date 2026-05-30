package sqlstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) loadUser(ctx context.Context, tenantID string, idOrAPIKey string) (*repository.UserRecord, error) {
	query := `
	SELECT u.id,
		u.tenant_id,
		t.slug,
		COALESCE((SELECT ak.key FROM api_keys ak WHERE ak.user_id = u.id ORDER BY ak.created_at LIMIT 1), ''),
		COALESCE((SELECT ak.project_id FROM api_keys ak WHERE ak.user_id = u.id ORDER BY ak.created_at LIMIT 1), ''),
		u.name,
		u.email,
		u.role,
		u.quota,
		u.used,
		u.qps,
		COALESCE((SELECT ak.budget_usd FROM api_keys ak WHERE ak.user_id = u.id ORDER BY ak.created_at LIMIT 1), 0),
		COALESCE((SELECT ak.spent_usd FROM api_keys ak WHERE ak.user_id = u.id ORDER BY ak.created_at LIMIT 1), 0),
		u.status,
		u.created_at,
		u.updated_at,
		COALESCE((SELECT ak.allowed_models FROM api_keys ak WHERE ak.user_id = u.id ORDER BY ak.created_at LIMIT 1), '')
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
	var allowedModelsRaw string
	if err := row.Scan(
		&user.ID,
		&user.TenantID,
		&user.TenantSlug,
		&user.APIKey,
		&user.ProjectID,
		&user.Name,
		&user.Email,
		&user.Role,
		&user.Quota,
		&user.Used,
		&user.QPS,
		&user.KeyBudgetUSD,
		&user.KeySpentUSD,
		&user.Status,
		&user.CreatedAt,
		&user.UpdatedAt,
		&allowedModelsRaw,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("load user: %w", err)
	}

	user.Models = decodeStringSlice(allowedModelsRaw)

	return &user, nil
}

func (s *Store) loadTenant(ctx context.Context, idOrSlug string) (*repository.TenantRecord, error) {
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
	SELECT id, slug, name, status, budget_usd, spent_usd, budget_policy, policy_body, created_at, updated_at
	FROM tenants
	WHERE id = ?
	   OR slug = ?
	LIMIT 1`), idOrSlug, idOrSlug)

	var tenant repository.TenantRecord
	var policyBody string
	if err := row.Scan(
		&tenant.ID,
		&tenant.Slug,
		&tenant.Name,
		&tenant.Status,
		&tenant.BudgetUSD,
		&tenant.SpentUSD,
		&tenant.BudgetPolicy,
		&policyBody,
		&tenant.CreatedAt,
		&tenant.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("load tenant: %w", err)
	}
	policy, err := decodeServicePolicy(policyBody)
	if err != nil {
		return nil, fmt.Errorf("decode tenant policy: %w", err)
	}
	tenant.Policy = policy

	return &tenant, nil
}

func encodeStringSlice(value []string) string {
	if len(value) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func decodeStringSlice(raw string) []string {
	if raw == "" {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil
	}
	return values
}

func encodeServicePolicy(policy *repository.ServicePolicyConfig) (string, error) {
	if policy == nil {
		return "{}", nil
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeServicePolicy(raw string) (*repository.ServicePolicyConfig, error) {
	if raw == "" || raw == "{}" {
		return nil, nil
	}
	var policy repository.ServicePolicyConfig
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return nil, err
	}
	return &policy, nil
}

var ErrInvalidJSON = errors.New("invalid JSON")

func validateJSON(columnName string, value string) error {
	if value == "" {
		return nil
	}
	if !json.Valid([]byte(value)) {
		return fmt.Errorf("invalid JSON for column %s: %w", columnName, ErrInvalidJSON)
	}
	return nil
}
