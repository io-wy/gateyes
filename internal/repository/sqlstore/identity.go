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
	ak.expires_at, ak.allowed_models, ak.allowed_providers, ak.allowed_services, ak.rate_limit_qps,
	u.id, u.name, u.email, u.status, u.quota, u.used, u.qps, u.role,
	t.id, t.slug, t.status, t.budget_usd, t.spent_usd, t.budget_policy,
	COALESCE(p.slug, ''), COALESCE(p.name, ''), COALESCE(p.status, ''), COALESCE(p.budget_usd, 0), COALESCE(p.spent_usd, 0), COALESCE(p.budget_policy, 'hard_reject')
FROM api_keys ak
JOIN users u ON u.id = ak.user_id
JOIN tenants t ON t.id = u.tenant_id
LEFT JOIN projects p ON p.id = ak.project_id
WHERE ak.key = ? OR ak.id = ?
LIMIT 1`), key, key)

	identity := &repository.AuthIdentity{}
	var apiKeyModelsRaw string
	var apiKeyProvidersRaw string
	var apiKeyServicesRaw string
	var apiKeyExpiresAt sql.NullTime
	if err := row.Scan(
		&identity.APIKeyID,
		&identity.APIKey,
		&identity.SecretHash,
		&identity.APIStatus,
		&identity.ProjectID,
		&identity.APIKeyBudgetUSD,
		&identity.APIKeySpentUSD,
		&identity.APIKeyBudgetPolicy,
		&apiKeyExpiresAt,
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

	identity.Models = decodeStringSlice(apiKeyModelsRaw)
	identity.APIKeyModels = decodeStringSlice(apiKeyModelsRaw)
	identity.APIKeyProviders = decodeStringSlice(apiKeyProvidersRaw)
	identity.APIKeyServices = decodeStringSlice(apiKeyServicesRaw)
	if apiKeyExpiresAt.Valid {
		value := apiKeyExpiresAt.Time
		identity.APIKeyExpiresAt = &value
	}

	return identity, nil
}

func (s *Store) TouchAPIKey(ctx context.Context, apiKeyID string, at time.Time) error {
	if apiKeyID == "" {
		return nil
	}
	if s.rdb != nil {
		value := at.UTC().Format(time.RFC3339Nano)
		_ = s.rdb.Set(ctx, lastUsedLatestKey(apiKeyID), value, lastUsedLatestTTL).Err()
		shouldWrite, err := s.rdb.SetNX(ctx, lastUsedDebounceKey(apiKeyID), value, lastUsedDebounceWindow).Result()
		if err == nil && !shouldWrite {
			return nil
		}
	}
	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE api_keys
SET last_used_at = ?, updated_at = ?
WHERE id = ?`), at.UTC(), at.UTC(), apiKeyID); err != nil {
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

func buildBudgetCheckResult(v *budgetCacheValue, estimatedCost float64, scope string) *repository.BudgetCheckResult {
	if v.BudgetUSD <= 0 {
		return &repository.BudgetCheckResult{Allowed: true, Policy: v.Policy, Scope: scope}
	}
	remaining := v.BudgetUSD - v.SpentUSD - estimatedCost
	return &repository.BudgetCheckResult{
		Allowed:   remaining >= 0,
		Scope:     scope,
		Policy:    v.Policy,
		Remaining: remaining,
	}
}

func (s *Store) CheckVirtualKeyBudget(ctx context.Context, virtualKeyID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	if virtualKeyID == "" {
		return &repository.BudgetCheckResult{Allowed: true}, nil
	}
	if cached, ok := s.budgetCacheGet(ctx, "virtual_key", virtualKeyID); ok {
		return buildBudgetCheckResult(cached, estimatedCost, "virtual_key"), nil
	}
	var budgetUSD, spentUSD float64
	var policy string
	if err := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
SELECT budget_usd, spent_usd, budget_policy FROM virtual_keys WHERE id = ?`), virtualKeyID).Scan(
		&budgetUSD, &spentUSD, &policy,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &repository.BudgetCheckResult{Allowed: true}, nil
		}
		return nil, fmt.Errorf("check virtual key budget: %w", err)
	}
	s.budgetCacheSet(ctx, "virtual_key", virtualKeyID, &budgetCacheValue{
		BudgetUSD: budgetUSD, SpentUSD: spentUSD, Policy: policy,
	})
	return buildBudgetCheckResult(&budgetCacheValue{
		BudgetUSD: budgetUSD, SpentUSD: spentUSD, Policy: policy,
	}, estimatedCost, "virtual_key"), nil
}

func (s *Store) ConsumeVirtualKeyBudget(ctx context.Context, virtualKeyID string, cost float64) (bool, error) {
	if virtualKeyID == "" || cost <= 0 {
		return true, nil
	}
	result, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
UPDATE virtual_keys
SET spent_usd = spent_usd + ?, updated_at = ?
WHERE id = ?
  AND (budget_usd <= 0 OR spent_usd + ? <= budget_usd)`),
		cost, time.Now().UTC(), virtualKeyID, cost,
	)
	if err != nil {
		return false, fmt.Errorf("consume virtual key budget: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("consume virtual key budget rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

func (s *Store) ConsumeBudgets(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID, userID string, cost float64, tokens int) (bool, error) {
	if cost <= 0 && tokens <= 0 {
		return true, nil
	}
	if ok, handled, err := s.consumeBudgetsRedis(ctx, apiKeyID, projectID, tenantID, virtualKeyID, userID, cost, tokens); handled {
		return ok, err
	}
	return s.consumeBudgetsPG(ctx, apiKeyID, projectID, tenantID, virtualKeyID, userID, cost, tokens)
}

func (s *Store) consumeBudgetsPG(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID, userID string, cost float64, tokens int) (bool, error) {
	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin budget tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()

	// Consume quota in the same transaction to ensure consistency.
	if tokens > 0 && userID != "" {
		result, err := tx.ExecContext(ctx, s.db.Rebind(`
			UPDATE users
			SET used = used + ?, updated_at = ?
			WHERE id = ?
			  AND (quota <= 0 OR used + ? <= quota)`),
			tokens, now, userID, tokens,
		)
		if err != nil {
			return false, fmt.Errorf("consume quota: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("consume quota rows: %w", err)
		}
		if rows == 0 {
			return false, repository.ErrQuotaExceeded
		}
	}

	if virtualKeyID != "" {
		result, err := tx.ExecContext(ctx, s.db.Rebind(`
	UPDATE virtual_keys
	SET spent_usd = spent_usd + ?, updated_at = ?
	WHERE id = ?
	  AND (budget_usd <= 0 OR spent_usd + ? <= budget_usd OR budget_policy != 'hard_reject')`),
			cost, now, virtualKeyID, cost,
		)
		if err != nil {
			return false, fmt.Errorf("consume virtual key budget: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("consume virtual key budget rows: %w", err)
		}
		if rows == 0 {
			return false, nil
		}
	}

	if apiKeyID != "" {
		result, err := tx.ExecContext(ctx, s.db.Rebind(`
	UPDATE api_keys
	SET spent_usd = spent_usd + ?, updated_at = ?
	WHERE id = ?
	  AND (budget_usd <= 0 OR spent_usd + ? <= budget_usd OR budget_policy != 'hard_reject')`),
			cost, now, apiKeyID, cost,
		)
		if err != nil {
			return false, fmt.Errorf("consume api key budget: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("consume api key budget rows: %w", err)
		}
		if rows == 0 {
			return false, nil
		}
	}

	if projectID != "" {
		result, err := tx.ExecContext(ctx, s.db.Rebind(`
	UPDATE projects
	SET spent_usd = spent_usd + ?, updated_at = ?
	WHERE id = ?
	  AND (budget_usd <= 0 OR spent_usd + ? <= budget_usd OR budget_policy != 'hard_reject')`),
			cost, now, projectID, cost,
		)
		if err != nil {
			return false, fmt.Errorf("consume project budget: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("consume project budget rows: %w", err)
		}
		if rows == 0 {
			return false, nil
		}
	}

	if tenantID != "" {
		result, err := tx.ExecContext(ctx, s.db.Rebind(`
	UPDATE tenants
	SET spent_usd = spent_usd + ?, updated_at = ?
	WHERE id = ?
	  AND (budget_usd <= 0 OR spent_usd + ? <= budget_usd OR budget_policy != 'hard_reject')`),
			cost, now, tenantID, cost,
		)
		if err != nil {
			return false, fmt.Errorf("consume tenant budget: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("consume tenant budget rows: %w", err)
		}
		if rows == 0 {
			return false, nil
		}
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit budget tx: %w", err)
	}
	// Invalidate cached budget snapshots so subsequent checks read fresh values.
	if virtualKeyID != "" {
		s.budgetCacheDelete(ctx, "virtual_key", virtualKeyID)
	}
	if apiKeyID != "" {
		s.budgetCacheDelete(ctx, "api_key", apiKeyID)
	}
	if projectID != "" {
		s.budgetCacheDelete(ctx, "project", projectID)
	}
	if tenantID != "" {
		s.budgetCacheDelete(ctx, "tenant", tenantID)
	}
	return true, nil
}

func budgetScopes(apiKeyID, projectID, tenantID, virtualKeyID string) []budgetScope {
	return []budgetScope{
		{ID: virtualKeyID, Scope: "virtual_key", Table: "virtual_keys"},
		{ID: apiKeyID, Scope: "api_key", Table: "api_keys"},
		{ID: projectID, Scope: "project", Table: "projects"},
		{ID: tenantID, Scope: "tenant", Table: "tenants"},
	}
}

// ReserveBudgets pre-authorizes budget across all scopes in a single transaction.
// Returns true if all scopes have sufficient available budget (budget - reserved - spent >= amount).
//
// TODO: This reserve/commit/release mechanism is implemented and tested but not
// yet wired into the request path. The current production flow uses ConsumeBudgets
// after the response is delivered. To enable true budget pre-authorization,
// replace the post-hoc ConsumeBudgets call in auth.recordUsage with
// ReserveBudgets -> upstream request -> CommitBudgets/ReleaseBudgets.
func (s *Store) ReserveBudgets(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64) (bool, error) {
	if amount <= 0 {
		return true, nil
	}
	if ok, handled, err := s.reserveBudgetsRedis(ctx, apiKeyID, projectID, tenantID, virtualKeyID, amount); handled {
		return ok, err
	}
	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin reserve tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	for _, sc := range budgetScopes(apiKeyID, projectID, tenantID, virtualKeyID) {
		if sc.ID == "" {
			continue
		}
		result, err := tx.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
			UPDATE %s
			SET reserved_usd = reserved_usd + ?, updated_at = ?
			WHERE id = ?
			  AND (budget_usd <= 0 OR spent_usd + reserved_usd + ? <= budget_usd OR budget_policy != 'hard_reject')
		`, sc.Table)), amount, now, sc.ID, amount)
		if err != nil {
			return false, fmt.Errorf("reserve %s budget: %w", sc.Table, err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return false, fmt.Errorf("reserve %s budget rows: %w", sc.Table, err)
		}
		if rows == 0 {
			return false, nil
		}
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit reserve tx: %w", err)
	}
	return true, nil
}

// CommitBudgets converts reserved budget to spent budget (reserve → spent).
func (s *Store) CommitBudgets(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64) error {
	if amount <= 0 {
		return nil
	}
	if handled, err := s.commitBudgetsRedis(ctx, apiKeyID, projectID, tenantID, virtualKeyID, amount); handled {
		return err
	}
	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin commit tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	for _, sc := range budgetScopes(apiKeyID, projectID, tenantID, virtualKeyID) {
		if sc.ID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
			UPDATE %s
			SET reserved_usd = reserved_usd - ?, spent_usd = spent_usd + ?, updated_at = ?
			WHERE id = ?
		`, sc.Table)), amount, amount, now, sc.ID); err != nil {
			return fmt.Errorf("commit %s budget: %w", sc.Table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit commit tx: %w", err)
	}
	return nil
}

// ReleaseBudgets releases pre-authorized budget without consuming it.
func (s *Store) ReleaseBudgets(ctx context.Context, apiKeyID, projectID, tenantID, virtualKeyID string, amount float64) error {
	if amount <= 0 {
		return nil
	}
	if handled, err := s.releaseBudgetsRedis(ctx, apiKeyID, projectID, tenantID, virtualKeyID, amount); handled {
		return err
	}
	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin release tx: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	for _, sc := range budgetScopes(apiKeyID, projectID, tenantID, virtualKeyID) {
		if sc.ID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
			UPDATE %s
			SET reserved_usd = reserved_usd - ?, updated_at = ?
			WHERE id = ?
		`, sc.Table)), amount, now, sc.ID); err != nil {
			return fmt.Errorf("release %s budget: %w", sc.Table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit release tx: %w", err)
	}
	return nil
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
	if rowsAffected > 0 {
		s.budgetCacheDelete(ctx, "tenant", tenantID)
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
SET secret_hash = ?, status = ?, project_id = ?, budget_usd = ?, allowed_models = ?, updated_at = ?
WHERE id = ?`), params.SecretHash, repository.StatusActive, params.ProjectID, params.KeyBudgetUSD, encodeStringSlice(params.Models), time.Now().UTC(), existing.APIKeyID); err != nil {
			tx.Rollback()
			return fmt.Errorf("update bootstrap key: %w", err)
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
INSERT INTO api_keys (id, user_id, key, secret_hash, status, project_id, budget_usd, spent_usd, allowed_models, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`),
		apiKeyID,
		userID,
		params.Key,
		params.SecretHash,
		repository.StatusActive,
		params.ProjectID,
		params.KeyBudgetUSD,
		encodeStringSlice(params.Models),
		now,
		now,
	); err != nil {
		tx.Rollback()
		return fmt.Errorf("insert bootstrap key: %w", err)
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
	if apiKeyID == "" {
		return &repository.BudgetCheckResult{Allowed: true, Scope: "api_key", Policy: repository.BudgetPolicyHardReject}, nil
	}
	if cached, ok := s.budgetCacheGet(ctx, "api_key", apiKeyID); ok {
		return buildBudgetCheckResult(cached, estimatedCost, "api_key"), nil
	}
	return s.checkBudget(ctx, "api_keys", apiKeyID, estimatedCost)
}

func (s *Store) CheckProjectBudget(ctx context.Context, projectID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	if projectID == "" {
		return &repository.BudgetCheckResult{Allowed: true, Scope: "project", Policy: repository.BudgetPolicyHardReject}, nil
	}
	if cached, ok := s.budgetCacheGet(ctx, "project", projectID); ok {
		return buildBudgetCheckResult(cached, estimatedCost, "project"), nil
	}
	return s.checkBudget(ctx, "projects", projectID, estimatedCost)
}

func (s *Store) CheckTenantBudget(ctx context.Context, tenantID string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	if cached, ok := s.budgetCacheGet(ctx, "tenant", tenantID); ok {
		return buildBudgetCheckResult(cached, estimatedCost, "tenant"), nil
	}
	return s.checkBudget(ctx, "tenants", tenantID, estimatedCost)
}

func (s *Store) checkBudget(ctx context.Context, table, id string, estimatedCost float64) (*repository.BudgetCheckResult, error) {
	var query string
	switch table {
	case "api_keys":
		query = "SELECT budget_usd, spent_usd, budget_policy FROM api_keys WHERE id = ?"
	case "projects":
		query = "SELECT budget_usd, spent_usd, budget_policy FROM projects WHERE id = ?"
	case "tenants":
		query = "SELECT budget_usd, spent_usd, budget_policy FROM tenants WHERE id = ?"
	default:
		return nil, fmt.Errorf("unsupported budget table: %s", table)
	}

	var budget, spent float64
	var policy string
	if err := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), id).Scan(&budget, &spent, &policy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &repository.BudgetCheckResult{Allowed: true, Scope: table, Policy: policy}, nil
		}
		return nil, fmt.Errorf("check %s budget: %w", table, err)
	}
	scope := table
	switch scope {
	case "api_keys":
		scope = "api_key"
	case "projects":
		scope = "project"
	case "tenants":
		scope = "tenant"
	case "virtual_keys":
		scope = "virtual_key"
	}
	s.budgetCacheSet(ctx, scope, id, &budgetCacheValue{
		BudgetUSD: budget, SpentUSD: spent, Policy: policy,
	})
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
		var budget, spent float64
		var policy string
		if err := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(
			`SELECT budget_usd, spent_usd, budget_policy FROM tenants WHERE id = ?`), tenantID,
		).Scan(&budget, &spent, &policy); err == nil {
			utilization := 0.0
			if budget > 0 {
				utilization = spent / budget
			}
			result = append(result, repository.BudgetStatus{
				Scope: "tenant", ID: tenantID, BudgetUSD: budget, SpentUSD: spent,
				Policy: policy, Utilization: utilization,
				IsExhausted: budget > 0 && spent >= budget,
			})
		}
	}
	if projectID != "" {
		var budget, spent float64
		var policy string
		if err := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(
			`SELECT budget_usd, spent_usd, budget_policy FROM projects WHERE id = ?`), projectID,
		).Scan(&budget, &spent, &policy); err == nil {
			utilization := 0.0
			if budget > 0 {
				utilization = spent / budget
			}
			result = append(result, repository.BudgetStatus{
				Scope: "project", ID: projectID, BudgetUSD: budget, SpentUSD: spent,
				Policy: policy, Utilization: utilization,
				IsExhausted: budget > 0 && spent >= budget,
			})
		}
	}
	if apiKeyID != "" {
		var budget, spent float64
		var policy string
		if err := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(
			`SELECT budget_usd, spent_usd, budget_policy FROM api_keys WHERE id = ?`), apiKeyID,
		).Scan(&budget, &spent, &policy); err == nil {
			utilization := 0.0
			if budget > 0 {
				utilization = spent / budget
			}
			result = append(result, repository.BudgetStatus{
				Scope: "api_key", ID: apiKeyID, BudgetUSD: budget, SpentUSD: spent,
				Policy: policy, Utilization: utilization,
				IsExhausted: budget > 0 && spent >= budget,
			})
		}
	}
	return result, nil
}
