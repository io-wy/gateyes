CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL,
	email TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL DEFAULT 'tenant_user',
	status TEXT NOT NULL,
	quota INTEGER NOT NULL,
	used INTEGER NOT NULL DEFAULT 0,
	qps INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);
CREATE INDEX IF NOT EXISTS idx_users_tenant_id ON users(tenant_id);
CREATE INDEX IF NOT EXISTS idx_users_tenant_role ON users(tenant_id, role);
