CREATE TABLE IF NOT EXISTS api_keys (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	project_id TEXT NOT NULL DEFAULT '',
	key TEXT NOT NULL UNIQUE,
	secret_hash TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	budget_usd REAL NOT NULL DEFAULT 0,
	spent_usd REAL NOT NULL DEFAULT 0,
	reserved_usd REAL NOT NULL DEFAULT 0,
	budget_policy TEXT NOT NULL DEFAULT 'hard_reject',
	allowed_models TEXT NOT NULL DEFAULT '[]',
	allowed_providers TEXT NOT NULL DEFAULT '[]',
	allowed_services TEXT NOT NULL DEFAULT '[]',
	rate_limit_qps INTEGER NOT NULL DEFAULT 0,
	last_used_at TIMESTAMP NULL,
	revoked_at TIMESTAMP NULL,
	expires_at TIMESTAMP NULL,
	rotated_at TIMESTAMP NULL,
	rotation_reminder_sent INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_project_id ON api_keys(project_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_status ON api_keys(status);
