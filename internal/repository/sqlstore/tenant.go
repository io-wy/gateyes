package sqlstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) ListTenants(ctx context.Context) ([]repository.TenantRecord, error) {
	rows, err := s.db.Conn.QueryContext(ctx, `
SELECT id, slug, name, status, budget_usd, spent_usd, policy_body, created_at, updated_at
FROM tenants
ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []repository.TenantRecord
	for rows.Next() {
		var tenant repository.TenantRecord
		var policyBody string
		if err := rows.Scan(
			&tenant.ID,
			&tenant.Slug,
			&tenant.Name,
			&tenant.Status,
			&tenant.BudgetUSD,
			&tenant.SpentUSD,
			&policyBody,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		tenant.Policy, err = decodeServicePolicy(policyBody)
		if err != nil {
			return nil, fmt.Errorf("decode tenant policy: %w", err)
		}
		tenants = append(tenants, tenant)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}

	return tenants, nil
}

func (s *Store) GetTenant(ctx context.Context, idOrSlug string) (*repository.TenantRecord, error) {
	return s.loadTenant(ctx, idOrSlug)
}

func (s *Store) UpdateTenant(ctx context.Context, idOrSlug string, params repository.UpdateTenantParams) (*repository.TenantRecord, error) {
	tenant, err := s.loadTenant(ctx, idOrSlug)
	if err != nil {
		return nil, err
	}

	sets := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if params.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *params.Name)
	}
	if params.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *params.Status)
	}
	if params.BudgetUSD != nil {
		sets = append(sets, "budget_usd = ?")
		args = append(args, *params.BudgetUSD)
	}
	if params.BudgetPolicy != nil {
		sets = append(sets, "budget_policy = ?")
		args = append(args, *params.BudgetPolicy)
	}
	if params.Policy != nil {
		policyBody, err := encodeServicePolicy(params.Policy)
		if err != nil {
			return nil, fmt.Errorf("encode tenant policy: %w", err)
		}
		sets = append(sets, "policy_body = ?")
		args = append(args, policyBody)
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC(), tenant.ID)

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
UPDATE tenants
SET %s
WHERE id = ?`, strings.Join(sets, ", "))), args...); err != nil {
		return nil, fmt.Errorf("update tenant: %w", err)
	}

	return s.loadTenant(ctx, tenant.ID)
}

func (s *Store) ListTenantProviders(ctx context.Context, tenantID string) ([]string, error) {
	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(`
SELECT provider_name
FROM tenant_providers
WHERE tenant_id = ?
  AND enabled = 1
ORDER BY provider_name`), tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant providers: %w", err)
	}
	defer rows.Close()

	var providerNames []string
	for rows.Next() {
		var providerName string
		if err := rows.Scan(&providerName); err != nil {
			return nil, fmt.Errorf("scan tenant provider: %w", err)
		}
		providerNames = append(providerNames, providerName)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant providers: %w", err)
	}

	return providerNames, nil
}

func (s *Store) ReplaceTenantProviders(ctx context.Context, tenantID string, providerNames []string) error {
	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin replace tenant providers: %w", err)
	}

	if _, err := tx.ExecContext(ctx, s.db.Rebind(`DELETE FROM tenant_providers WHERE tenant_id = ?`), tenantID); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete tenant providers: %w", err)
	}

	now := time.Now().UTC()
	for _, providerName := range providerNames {
		if _, err := tx.ExecContext(ctx, s.db.Rebind(`
INSERT INTO tenant_providers (id, tenant_id, provider_name, enabled, created_at, updated_at)
VALUES (?, ?, ?, 1, ?, ?)`), uuid.NewString(), tenantID, providerName, now, now); err != nil {
			tx.Rollback()
			return fmt.Errorf("insert tenant provider: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tenant providers: %w", err)
	}

	return nil
}

func (s *Store) BackfillDefaultTenant(ctx context.Context, tenantID string) error {
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE users
SET tenant_id = ?, updated_at = ?
WHERE tenant_id = ''`), tenantID, time.Now().UTC()); err != nil {
		return fmt.Errorf("backfill users tenant: %w", err)
	}

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE usage_records
SET tenant_id = ?
WHERE tenant_id = ''`), tenantID); err != nil {
		return fmt.Errorf("backfill usage tenant: %w", err)
	}

	return nil
}
