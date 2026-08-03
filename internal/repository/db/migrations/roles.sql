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

INSERT INTO roles (id, tenant_id, name, description, is_system, created_at, updated_at) VALUES
('role_super_admin', '', 'super_admin', 'Platform super administrator', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('role_tenant_admin', '', 'tenant_admin', 'Tenant administrator', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
('role_tenant_user', '', 'tenant_user', 'Regular tenant user', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO NOTHING;
