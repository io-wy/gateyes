CREATE TABLE IF NOT EXISTS plugins (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	name TEXT NOT NULL,
	type TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	author TEXT NOT NULL DEFAULT '',
	phases TEXT NOT NULL DEFAULT '[]',
	file_path TEXT NOT NULL DEFAULT '',
	address TEXT NOT NULL DEFAULT '',
	timeout_ms INTEGER NOT NULL DEFAULT 50,
	memory_pages INTEGER NOT NULL DEFAULT 1,
	enabled INTEGER NOT NULL DEFAULT 0,
	source TEXT NOT NULL DEFAULT 'custom',
	config_body TEXT NOT NULL DEFAULT '{}',
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_plugins_tenant_name ON plugins(tenant_id, name);
CREATE INDEX IF NOT EXISTS idx_plugins_tenant_enabled ON plugins(tenant_id, enabled);
CREATE INDEX IF NOT EXISTS idx_plugins_tenant_type ON plugins(tenant_id, type);
