-- RBAC tables: replace hard-coded rolePermissions map with database-backed roles and permissions.

CREATE TABLE IF NOT EXISTS roles (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_system INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_tenant_name ON roles(tenant_id, name);

CREATE TABLE IF NOT EXISTS permissions (
    id TEXT PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id TEXT NOT NULL,
    permission_id TEXT NOT NULL,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    PRIMARY KEY (user_id, role_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_user_roles_user ON user_roles(user_id);
CREATE INDEX IF NOT EXISTS idx_user_roles_tenant ON user_roles(tenant_id);

-- Seed default permissions. The list mirrors middleware.Permission constants.
INSERT INTO permissions (id, code, name) VALUES
('perm_provider_read', 'provider:read', 'Read providers'),
('perm_provider_write', 'provider:write', 'Write providers'),
('perm_api_key_read', 'api_key:read', 'Read API keys'),
('perm_api_key_write', 'api_key:write', 'Write API keys'),
('perm_user_read', 'user:read', 'Read users'),
('perm_user_write', 'user:write', 'Write users'),
('perm_tenant_read', 'tenant:read', 'Read tenants'),
('perm_tenant_write', 'tenant:write', 'Write tenants'),
('perm_project_read', 'project:read', 'Read projects'),
('perm_project_write', 'project:write', 'Write projects'),
('perm_service_read', 'service:read', 'Read services'),
('perm_service_write', 'service:write', 'Write services'),
('perm_virtual_key_read', 'virtual_key:read', 'Read virtual keys'),
('perm_virtual_key_write', 'virtual_key:write', 'Write virtual keys'),
('perm_usage_read', 'usage:read', 'Read usage'),
('perm_response_read', 'response:read', 'Read responses'),
('perm_budget_read', 'budget:read', 'Read budgets'),
('perm_audit_read', 'audit:read', 'Read audit logs'),
('perm_config_write', 'config:write', 'Write config')
ON CONFLICT (code) DO NOTHING;

-- Seed system roles.
INSERT INTO roles (id, tenant_id, name, description, is_system, created_at, updated_at) VALUES
('role_super_admin', '', 'super_admin', 'Platform super administrator', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('role_tenant_admin', '', 'tenant_admin', 'Tenant administrator', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('role_tenant_user', '', 'tenant_user', 'Regular tenant user', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO NOTHING;

-- Map system roles to permissions (mirrors the legacy rolePermissions map).
INSERT INTO role_permissions (role_id, permission_id)
SELECT 'role_super_admin', id FROM permissions
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 'role_tenant_admin', id FROM permissions WHERE code != 'config:write'
ON CONFLICT (role_id, permission_id) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT 'role_tenant_user', id FROM permissions WHERE code = 'usage:read'
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- Migrate existing users: create a user_roles row from the legacy User.Role column.
INSERT INTO user_roles (user_id, role_id, tenant_id)
SELECT
    u.id,
    CASE u.role
        WHEN 'super_admin' THEN 'role_super_admin'
        WHEN 'tenant_admin' THEN 'role_tenant_admin'
        ELSE 'role_tenant_user'
    END,
    u.tenant_id
FROM users u
ON CONFLICT (user_id, role_id, tenant_id) DO NOTHING;
