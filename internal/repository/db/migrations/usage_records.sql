CREATE TABLE IF NOT EXISTS usage_records (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL DEFAULT '',
	project_id TEXT NOT NULL DEFAULT '',
	user_id TEXT NOT NULL,
	api_key_id TEXT NOT NULL,
	provider_name TEXT NOT NULL,
	model TEXT NOT NULL,
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	total_tokens INTEGER NOT NULL DEFAULT 0,
	cost REAL NOT NULL DEFAULT 0,
	latency_ms INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL,
	error_type TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_usage_records_user_created_at ON usage_records(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_created_at ON usage_records(tenant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_provider_created_at ON usage_records(tenant_id, provider_name, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_user ON usage_records(tenant_id, user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_project ON usage_records(tenant_id, project_id, created_at);
CREATE INDEX IF NOT EXISTS idx_usage_records_tenant_model ON usage_records(tenant_id, model, created_at);
