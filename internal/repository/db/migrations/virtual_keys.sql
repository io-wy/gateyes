CREATE TABLE IF NOT EXISTS virtual_keys (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL,
	project_id TEXT NOT NULL DEFAULT '',
	user_id TEXT NOT NULL,
	api_key_id TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	key TEXT NOT NULL,
	secret_hash TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'active',
	budget_usd REAL NOT NULL DEFAULT 0,
	spent_usd REAL NOT NULL DEFAULT 0,
	reserved_usd REAL NOT NULL DEFAULT 0,
	budget_policy TEXT NOT NULL DEFAULT 'hard_reject',
	rate_limit_qps INTEGER NOT NULL DEFAULT 0,
	allowed_models TEXT NOT NULL DEFAULT '[]',
	allowed_providers TEXT NOT NULL DEFAULT '[]',
	metadata TEXT NOT NULL DEFAULT '{}',
	callback_url TEXT NOT NULL DEFAULT '',
	expires_at TIMESTAMP NULL,
	revoked_at TIMESTAMP NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_virtual_keys_key ON virtual_keys(key);
CREATE INDEX IF NOT EXISTS idx_virtual_keys_tenant ON virtual_keys(tenant_id);
CREATE INDEX IF NOT EXISTS idx_virtual_keys_api_key ON virtual_keys(api_key_id);
CREATE INDEX IF NOT EXISTS idx_virtual_keys_project_id ON virtual_keys(project_id);
CREATE INDEX IF NOT EXISTS idx_virtual_keys_status ON virtual_keys(status);
