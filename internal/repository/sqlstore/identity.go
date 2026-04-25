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
SELECT ak.id, ak.key, ak.secret_hash, ak.status, ak.project_id, ak.budget_usd, ak.spent_usd, ak.budget_policy,
	ak.allowed_models, ak.allowed_providers, ak.allowed_services, ak.rate_limit_qps,
	u.id, u.name, u.email, u.status, u.quota, u.used, u.qps, u.role,
	t.id, t.slug, t.status, t.budget_usd, t.spent_usd, t.budget_policy,
	COALESCE(p.slug, ''), COALESCE(p.name, ''), COALESCE(p.status, ''), COALESCE(p.budget_usd, 0), COALESCE(p.spent_usd, 0), COALESCE(p.budget_policy, 'hard_reject')
FROM api_keys ak
JOIN users u ON u.id = ak.user_id
JOIN tenants t ON t.id = u.tenant_id
LEFT JOIN projects p ON p.id = ak.project_id
WHERE ak.key = ?
LIMIT 1`), key)

	identity := &repository.AuthIdentity{}
	var apiKeyModelsRaw string
	var apiKeyProvidersRaw string
	var apiKeyServicesRaw string
	if err := row.Scan(
		&identity.APIKeyID,
		&identity.APIKey,
		&identity.SecretHash,
		&identity.APIStatus,
		&identity.ProjectID,
		&identity.APIKeyBudgetUSD,
		&identity.APIKeySpentUSD,
		&identity.APIKeyBudgetPolicy,
		&apiKeyModelsRaw,
		&apiKeyProvidersRaw,
		&apiKeyServicesRaw,
		&identity.APIKeyRateLimitQPS,
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
		&identity.TenantBudgetUSD,
		&identity.TenantSpentUSD,
		&identity.TenantBudgetPolicy,
		&identity.ProjectSlug,
		&identity.ProjectName,
		&identity.ProjectStatus,
		&identity.ProjectBudgetUSD,
		&identity.ProjectSpentUSD,
		&identity.ProjectBudgetPolicy,
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
	identity.APIKeyModels = decodeStringSlice(apiKeyModelsRaw)
	identity.APIKeyProviders = decodeStringSlice(apiKeyProvidersRaw)
	identity.APIKeyServices = decodeStringSlice(apiKeyServicesRaw)

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

func (s *Store) ConsumeAPIKeyBudget(ctx context.Context, apiKeyID string, cost float64) (bool, error) {
	if apiKeyID == "" || cost <= 0 {
		return true, nil
	}
	result, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE api_keys
SET spent_usd = spent_usd + ?, updated_at = ?
WHERE id = ?
  AND (budget_usd <= 0 OR spent_usd + ? <= budget_usd)`),
		cost, time.Now().UTC(), apiKeyID, cost,
	)
	if err != nil {
		return false, fmt.Errorf("consume api key budget: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("consume api key budget rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

func (s *Store) ConsumeProjectBudget(ctx context.Context, projectID string, cost float64) (bool, error) {
	if projectID == "" || cost <= 0 {
		return true, nil
	}
	result, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE projects
SET spent_usd = spent_usd + ?, updated_at = ?
WHERE id = ?
  AND (budget_usd <= 0 OR spent_usd + ? <= budget_usd)`),
		cost, time.Now().UTC(), projectID, cost,
	)
	if err != nil {
		return false, fmt.Errorf("consume project budget: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("consume project budget rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

func (s *Store) ConsumeTenantBudget(ctx context.Context, tenantID string, cost float64) (bool, error) {
	if tenantID == "" || cost <= 0 {
		return true, nil
	}
	result, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE tenants
SET spent_usd = spent_usd + ?, updated_at = ?
WHERE id = ?
  AND (budget_usd <= 0 OR spent_usd + ? <= budget_usd)`),
		cost, time.Now().UTC(), tenantID, cost,
	)
	if err != nil {
		return false, fmt.Errorf("consume tenant budget: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("consume tenant budget rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

func (s *Store) EnsureBootstrapKey(ctx context.Context, params repository.BootstrapAPIKeyParams) error {
	if _, err := s.loadTenant(ctx, params.TenantID); err != nil {
		return err
	}
	if params.ProjectID != "" {
		if _, err := s.loadProject(ctx, params.TenantID, params.ProjectID); err != nil {
			return err
		}
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
SET secret_hash = ?, status = ?, project_id = ?, budget_usd = ?, updated_at = ?
WHERE id = ?`), params.SecretHash, repository.StatusActive, params.ProjectID, params.KeyBudgetUSD, time.Now().UTC(), existing.APIKeyID); err != nil {
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
INSERT INTO api_keys (id, user_id, key, secret_hash, status, project_id, budget_usd, spent_usd, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`),
		apiKeyID,
		userID,
		params.Key,
		params.SecretHash,
		repository.StatusActive,
		params.ProjectID,
		params.KeyBudgetUSD,
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

func (s *Store) CheckAPIKeyBudget(ctx context.Context, apiKeyID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return s.checkBudget(ctx, "api_keys", apiKeyID, estimatedCost)
}

func (s *Store) CheckProjectBudget(ctx context.Context, projectID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	if projectID == "" {
		return &repository.BudgetCheckResult{Allowed: true, Scope: "project", Policy: repository.BudgetPolicyHardReject}, nil
	}
	return s.checkBudget(ctx, "projects", projectID, estimatedCost)
}

func (s *Store) CheckTenantBudget(ctx context.Context, tenantID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	return s.checkBudget(ctx, "tenants", tenantID, estimatedCost)
}

func (s *Store) checkBudget(ctx context.Context, table, id string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	var budget, spent float64
	var policy string
	if err := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(
		fmt.Sprintf(`SELECT budget_usd, spent_usd, budget_policy FROM %s WHERE id = ?`, table)), id,
	).Scan(&budget, &spent, &policy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &repository.BudgetCheckResult{Allowed: true, Scope: table, Policy: policy}, nil
		}
		return nil, fmt.Errorf("check %s budget: %w", table, err)
	}
	if budget <= 0 {
		return &repository.BudgetCheckResult{Allowed: true, Scope: table, Policy: policy, Remaining: -1}, nil
	}
	remaining := budget - spent - estimatedCost
	allowed := remaining >= 0
	return &repository.BudgetCheckResult{Allowed: allowed, Scope: table, Policy: policy, Remaining: remaining}, nil
}

func (s *Store) GetBudgetStatus(ctx context.Context, tenantID, projectID, apiKeyID string) ([]repository.BudgetStatus, error) {
	var result []repository.BudgetStatus
	if tenantID != "" {
		var budget, spent, overage float64
		var policy string
		if err := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(
			`SELECT budget_usd, spent_usd, overage_usd, budget_policy FROM tenants WHERE id = ?`), tenantID,
		).Scan(&budget, &spent, &overage, &policy); err == nil {
			utilization := 0.0
			if budget > 0 {
				utilization = spent / budget
			}
			result = append(result, repository.BudgetStatus{
				Scope: "tenant", ID: tenantID, BudgetUSD: budget, SpentUSD: spent,
				OverageUSD: overage, Policy: policy, Utilization: utilization,
				IsExhausted: budget > 0 && spent >= budget,
			})
		}
	}
	if projectID != "" {
		var budget, spent, overage float64
		var policy string
		if err := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(
			`SELECT budget_usd, spent_usd, overage_usd, budget_policy FROM projects WHERE id = ?`), projectID,
		).Scan(&budget, &spent, &overage, &policy); err == nil {
			utilization := 0.0
			if budget > 0 {
				utilization = spent / budget
			}
			result = append(result, repository.BudgetStatus{
				Scope: "project", ID: projectID, BudgetUSD: budget, SpentUSD: spent,
				OverageUSD: overage, Policy: policy, Utilization: utilization,
				IsExhausted: budget > 0 && spent >= budget,
			})
		}
	}
	if apiKeyID != "" {
		var budget, spent, overage float64
		var policy string
		if err := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(
			`SELECT budget_usd, spent_usd, overage_usd, budget_policy FROM api_keys WHERE id = ?`), apiKeyID,
		).Scan(&budget, &spent, &overage, &policy); err == nil {
			utilization := 0.0
			if budget > 0 {
				utilization = spent / budget
			}
			result = append(result, repository.BudgetStatus{
				Scope: "api_key", ID: apiKeyID, BudgetUSD: budget, SpentUSD: spent,
				OverageUSD: overage, Policy: policy, Utilization: utilization,
				IsExhausted: budget > 0 && spent >= budget,
			})
		}
	}
	return result, nil
}
