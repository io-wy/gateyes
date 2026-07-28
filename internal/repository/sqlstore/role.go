package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gateyes/gateway/internal/repository"
)

func (s *Store) CreateRole(ctx context.Context, params repository.CreateRoleParams) (*repository.RoleRecord, error) {
	if params.Name == "" {
		return nil, fmt.Errorf("role name is required")
	}

	now := time.Now().UTC()
	roleID := uuid.NewString()

	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create role: %w", err)
	}

	if _, err := tx.ExecContext(ctx, s.db.Rebind(`
INSERT INTO roles (id, tenant_id, name, description, is_system, created_at, updated_at)
VALUES (?, ?, ?, ?, 0, ?, ?)`),
		roleID, params.TenantID, params.Name, params.Description, now, now,
	); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("insert role: %w", err)
	}

	if len(params.PermissionIDs) > 0 {
		if err := s.setRolePermissionsTx(ctx, tx, roleID, params.PermissionIDs); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create role: %w", err)
	}

	return s.GetRole(ctx, params.TenantID, roleID)
}

func (s *Store) ListRoles(ctx context.Context, tenantID string, filter repository.RoleFilter) ([]repository.RoleRecord, error) {
	query := `
SELECT id, tenant_id, name, description, is_system, created_at, updated_at
FROM roles
WHERE tenant_id = ''`
	args := []any{}

	if tenantID != "" {
		query += ` OR tenant_id = ?`
		args = append(args, tenantID)
	} else if !filter.IncludeSystem {
		// If no tenant filter and system roles excluded, return nothing meaningful.
		query += ` AND 1 = 0`
	}

	query += ` ORDER BY created_at DESC`

	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(query), args...)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	defer rows.Close()

	var roles []repository.RoleRecord
	for rows.Next() {
		var role repository.RoleRecord
		var isSystem int
		if err := rows.Scan(
			&role.ID,
			&role.TenantID,
			&role.Name,
			&role.Description,
			&isSystem,
			&role.CreatedAt,
			&role.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		role.IsSystem = isSystem != 0
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate roles: %w", err)
	}

	for i := range roles {
		perms, err := s.GetRolePermissions(ctx, roles[i].ID)
		if err != nil {
			return nil, err
		}
		for _, p := range perms {
			roles[i].Permissions = append(roles[i].Permissions, p.Code)
		}
	}

	return roles, nil
}

func (s *Store) GetRole(ctx context.Context, tenantID string, id string) (*repository.RoleRecord, error) {
	row := s.db.Conn.QueryRowContext(ctx, s.db.Rebind(`
SELECT id, tenant_id, name, description, is_system, created_at, updated_at
FROM roles
WHERE id = ?`), id)

	var role repository.RoleRecord
	var isSystem int
	if err := row.Scan(
		&role.ID,
		&role.TenantID,
		&role.Name,
		&role.Description,
		&isSystem,
		&role.CreatedAt,
		&role.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("get role: %w", err)
	}
	role.IsSystem = isSystem != 0

	if role.TenantID != "" && role.TenantID != tenantID {
		return nil, repository.ErrNotFound
	}

	perms, err := s.GetRolePermissions(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, p := range perms {
		role.Permissions = append(role.Permissions, p.Code)
	}

	return &role, nil
}

func (s *Store) UpdateRole(ctx context.Context, tenantID string, id string, params repository.UpdateRoleParams) (*repository.RoleRecord, error) {
	role, err := s.GetRole(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if role.IsSystem {
		return nil, fmt.Errorf("cannot modify system role")
	}

	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update role: %w", err)
	}

	sets := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if params.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *params.Name)
	}
	if params.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *params.Description)
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC(), id)

	if _, err := tx.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
UPDATE roles
SET %s
WHERE id = ?`, strings.Join(sets, ", "))), args...); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("update role: %w", err)
	}

	if params.PermissionIDs != nil {
		if err := s.setRolePermissionsTx(ctx, tx, id, *params.PermissionIDs); err != nil {
			tx.Rollback()
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update role: %w", err)
	}

	return s.GetRole(ctx, tenantID, id)
}

func (s *Store) DeleteRole(ctx context.Context, tenantID string, id string) error {
	role, err := s.GetRole(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return fmt.Errorf("cannot delete system role")
	}

	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete role: %w", err)
	}

	if _, err := tx.ExecContext(ctx, s.db.Rebind(`DELETE FROM user_roles WHERE role_id = ?`), id); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete user_roles: %w", err)
	}
	if _, err := tx.ExecContext(ctx, s.db.Rebind(`DELETE FROM role_permissions WHERE role_id = ?`), id); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete role_permissions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, s.db.Rebind(`DELETE FROM roles WHERE id = ?`), id); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete role: %w", err)
	}

	return tx.Commit()
}

func (s *Store) ListPermissions(ctx context.Context) ([]repository.PermissionRecord, error) {
	rows, err := s.db.Conn.QueryContext(ctx, `
SELECT id, code, name, description
FROM permissions
ORDER BY code`)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	defer rows.Close()

	var perms []repository.PermissionRecord
	for rows.Next() {
		var p repository.PermissionRecord
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Description); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		perms = append(perms, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate permissions: %w", err)
	}
	return perms, nil
}

func (s *Store) GetRolePermissions(ctx context.Context, roleID string) ([]repository.PermissionRecord, error) {
	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(`
SELECT p.id, p.code, p.name, p.description
FROM permissions p
JOIN role_permissions rp ON rp.permission_id = p.id
WHERE rp.role_id = ?
ORDER BY p.code`), roleID)
	if err != nil {
		return nil, fmt.Errorf("get role permissions: %w", err)
	}
	defer rows.Close()

	var perms []repository.PermissionRecord
	for rows.Next() {
		var p repository.PermissionRecord
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Description); err != nil {
			return nil, fmt.Errorf("scan role permission: %w", err)
		}
		perms = append(perms, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate role permissions: %w", err)
	}
	return perms, nil
}

func (s *Store) SetRolePermissions(ctx context.Context, roleID string, permissionIDs []string) error {
	tx, err := s.db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin set role permissions: %w", err)
	}
	if err := s.setRolePermissionsTx(ctx, tx, roleID, permissionIDs); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) setRolePermissionsTx(ctx context.Context, tx *sql.Tx, roleID string, permissionIDs []string) error {
	if _, err := tx.ExecContext(ctx, s.db.Rebind(`DELETE FROM role_permissions WHERE role_id = ?`), roleID); err != nil {
		return fmt.Errorf("clear role permissions: %w", err)
	}
	if len(permissionIDs) == 0 {
		return nil
	}

	placeholders := make([]string, len(permissionIDs))
	args := make([]any, 0, len(permissionIDs)*2)
	for i, pid := range permissionIDs {
		placeholders[i] = "(?, ?)"
		args = append(args, roleID, pid)
	}

	if _, err := tx.ExecContext(ctx, s.db.Rebind(fmt.Sprintf(`
INSERT INTO role_permissions (role_id, permission_id)
VALUES %s`, strings.Join(placeholders, ", "))), args...); err != nil {
		return fmt.Errorf("insert role permissions: %w", err)
	}
	return nil
}

func (s *Store) GetUserRoleIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(`
SELECT role_id FROM user_roles WHERE user_id = ?`), userID)
	if err != nil {
		return nil, fmt.Errorf("get user role ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan user role id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user role ids: %w", err)
	}
	return ids, nil
}

func (s *Store) GetUserPermissions(ctx context.Context, userID string) ([]repository.PermissionRecord, error) {
	rows, err := s.db.Conn.QueryContext(ctx, s.db.Rebind(`
SELECT DISTINCT p.id, p.code, p.name, p.description
FROM permissions p
JOIN role_permissions rp ON rp.permission_id = p.id
JOIN user_roles ur ON ur.role_id = rp.role_id
WHERE ur.user_id = ?
ORDER BY p.code`), userID)
	if err != nil {
		return nil, fmt.Errorf("get user permissions: %w", err)
	}
	defer rows.Close()

	var perms []repository.PermissionRecord
	for rows.Next() {
		var p repository.PermissionRecord
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Description); err != nil {
			return nil, fmt.Errorf("scan user permission: %w", err)
		}
		perms = append(perms, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user permissions: %w", err)
	}
	return perms, nil
}
