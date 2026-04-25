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

func (s *Store) CreateProject(ctx context.Context, params repository.CreateProjectParams) (*repository.ProjectRecord, error) {
	tenant, err := s.loadTenant(ctx, params.TenantID)
	if err != nil {
		return nil, err
	}
	policyBody, err := encodeServicePolicy(params.Policy)
	if err != nil {
		return nil, fmt.Errorf("encode project policy: %w", err)
	}

	now := time.Now().UTC()
	record := repository.ProjectRecord{
		ID:         uuid.NewString(),
		TenantID:   tenant.ID,
		TenantSlug: tenant.Slug,
		Slug:       params.Slug,
		Name:       params.Name,
		Status:     firstNonEmptyStatus(params.Status),
		BudgetUSD:  params.BudgetUSD,
		SpentUSD:   0,
		CreatedAt:  now,
		UpdatedAt:  now,
		Policy:     params.Policy,
	}

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(`
INSERT INTO projects (id, tenant_id, slug, name, status, budget_usd, spent_usd, policy_body, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		record.ID, record.TenantID, record.Slug, record.Name, record.Status, record.BudgetUSD, record.SpentUSD, policyBody, record.CreatedAt, record.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return s.GetProject(ctx, tenant.ID, record.ID)
}

func (s *Store) ListProjects(ctx context.Context, tenantID string) ([]repository.ProjectRecord, error) {
	query := `
SELECT p.id, p.tenant_id, t.slug, p.slug, p.name, p.status, p.budget_usd, p.spent_usd, p.budget_policy, p.policy_body, p.created_at, p.updated_at
FROM projects p
JOIN tenants t ON t.id = p.tenant_id`
	args := make([]any, 0, 1)
	if tenantID != "" {
		query += `
WHERE p.tenant_id = ?`
		args = append(args, tenantID)
	}
	query += `
ORDER BY p.created_at ASC`

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var items []repository.ProjectRecord
	for rows.Next() {
		record, err := scanProjectRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return items, nil
}

func (s *Store) GetProject(ctx context.Context, tenantID string, idOrSlug string) (*repository.ProjectRecord, error) {
	return s.loadProject(ctx, tenantID, idOrSlug)
}

func (s *Store) UpdateProject(ctx context.Context, tenantID string, idOrSlug string, params repository.UpdateProjectParams) (*repository.ProjectRecord, error) {
	project, err := s.loadProject(ctx, tenantID, idOrSlug)
	if err != nil {
		return nil, err
	}

	sets := make([]string, 0, 4)
	args := make([]any, 0, 5)
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
			return nil, fmt.Errorf("encode project policy: %w", err)
		}
		sets = append(sets, "policy_body = ?")
		args = append(args, policyBody)
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC(), project.ID)

	if _, err := s.db.Conn.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
UPDATE projects
SET %s
WHERE id = ?`, strings.Join(sets, ", "))), args...); err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	return s.GetProject(ctx, project.TenantID, project.ID)
}

func (s *Store) loadProject(ctx context.Context, tenantID string, idOrSlug string) (*repository.ProjectRecord, error) {
	query := `
SELECT p.id, p.tenant_id, t.slug, p.slug, p.name, p.status, p.budget_usd, p.spent_usd, p.budget_policy, p.policy_body, p.created_at, p.updated_at
FROM projects p
JOIN tenants t ON t.id = p.tenant_id
WHERE (p.id = ? OR p.slug = ?)`
	args := []any{idOrSlug, idOrSlug}
	if tenantID != "" {
		query += `
  AND p.tenant_id = ?`
		args = append(args, tenantID)
	}
	query += `
LIMIT 1`

	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(query), args...)
	record, err := scanProjectRecord(row)
	if err == sql.ErrNoRows {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load project: %w", err)
	}
	return record, nil
}

func scanProjectRecord(scanner rowScanner) (*repository.ProjectRecord, error) {
	record := &repository.ProjectRecord{}
	var policyBody string
	if err := scanner.Scan(
		&record.ID,
		&record.TenantID,
		&record.TenantSlug,
		&record.Slug,
		&record.Name,
		&record.Status,
		&record.BudgetUSD,
		&record.SpentUSD,
		&record.BudgetPolicy,
		&policyBody,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	var err error
	record.Policy, err = decodeServicePolicy(policyBody)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func firstNonEmptyStatus(value string) string {
	if strings.TrimSpace(value) == "" {
		return repository.StatusActive
	}
	return value
}
