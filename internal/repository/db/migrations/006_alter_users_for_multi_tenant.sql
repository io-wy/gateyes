ALTER TABLE users ADD COLUMN tenant_id TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'tenant_user';

CREATE INDEX IF NOT EXISTS idx_users_tenant_id ON users(tenant_id);
CREATE INDEX IF NOT EXISTS idx_users_tenant_role ON users(tenant_id, role);
